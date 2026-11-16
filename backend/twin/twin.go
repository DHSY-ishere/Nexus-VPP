// Package twin is L6: the grid digital twin's analytics.
//
//   - ShapleyAttribution implements the counterfactual stabilisation credit: each
//     contributor is credited with its sampled Shapley share of the stress the
//     market removed compared with the "nobody helped" counterfactual.
//   - SensitivityLearner and LearnedGate implement the model-free network gate:
//     homes estimate their own voltage-to-power sensitivity by small perturbations,
//     and the gate rebuilds feeder voltages from those estimates alone.
package twin

import (
	"math"
	"math/rand"
)

// Effect is a signed change in a bus's injection (kW).
type Effect struct {
	Bus int
	KW  float64
}

// Stresser is anything that can score how far a set of bus injections is outside
// its limits: the true feeder model, or a LearnedNetwork.
type Stresser interface {
	Stress(inj []float64) float64
}

// Contribution is one player in the attribution game: a trade, whose seller and
// buyer legs are applied together. A single leg is not energy-neutral and would
// split into large offsetting shares, so legs are never separated.
type Contribution struct {
	ID      string
	Effects []Effect
}

// ShapleyAttribution returns each contribution's share of the stress reduction achieved by
// all contributions together. Marginal contributions are averaged over random
// orderings, so the shares always sum to the total improvement (Shapley efficiency)
// and a leg that makes things worse gets a negative share.
// total is the improvement of the full set over the empty set.
func ShapleyAttribution(f Stresser, baseline []float64, contribs []Contribution, samples int, rng *rand.Rand) (shares map[string]float64, total float64) {
	shares = map[string]float64{}
	if len(contribs) == 0 || samples <= 0 {
		return shares, 0
	}
	base := f.Stress(baseline)
	inj := make([]float64, len(baseline))
	idx := make([]int, len(contribs))
	for i := range idx {
		idx[i] = i
	}
	for s := 0; s < samples; s++ {
		rng.Shuffle(len(idx), func(a, b int) { idx[a], idx[b] = idx[b], idx[a] })
		copy(inj, baseline)
		prev := base
		for _, i := range idx {
			c := contribs[i]
			for _, ef := range c.Effects {
				inj[ef.Bus] += ef.KW
			}
			cur := f.Stress(inj)
			shares[c.ID] += (prev - cur) / float64(samples)
			prev = cur
		}
		if s == 0 {
			total = base - prev
		}
	}
	return shares, total
}

// SensitivityLearner estimates dV/dP for one home by least squares through the
// origin over small perturbation probes.
type SensitivityLearner struct {
	sumXY, sumXX float64
	N            int
}

// Observe records that changing power by dPkW moved the home's voltage by dV volts.
func (l *SensitivityLearner) Observe(dPkW, dV float64) {
	l.sumXY += dPkW * dV
	l.sumXX += dPkW * dPkW
	l.N++
}

// Estimate returns volts per kW (0 until a probe has been observed).
func (l *SensitivityLearner) Estimate() float64 {
	if l.sumXX == 0 {
		return 0
	}
	return l.sumXY / l.sumXX
}

// LearnedGate computes bus voltages from learned self-sensitivities only. On a
// radial feeder dV_i/dP_j equals the self-sensitivity of the bus nearer the
// transformer, so no impedance model is needed.
type LearnedGate struct {
	NominalV float64
	Self     []float64 // learned dV_i/dP_i per bus, volts per kW
}

// Voltages estimates the voltage at each bus.
func (g LearnedGate) Voltages(inj []float64) []float64 {
	v := make([]float64, len(g.Self))
	for i := range v {
		v[i] = g.NominalV
		for j := range g.Self {
			if j < len(inj) {
				v[i] += g.Self[min(i, j)] * inj[j]
			}
		}
	}
	return v
}

// LearnedNetwork is a network gate that needs no impedance model. Thermal limits
// come from cable ratings (nameplate data the operator has); voltage comes from the
// self-sensitivities homes learned by probing. It implements the same Stress and
// PathResistance used by the market gate as the true feeder model does.
type LearnedNetwork struct {
	NominalV   float64
	MinPU      float64
	MaxPU      float64
	MarginPU   float64   // safety margin against estimation error, in per unit
	RatedKW    []float64 // per segment, ordered from the transformer
	Self       []float64 // learned dV_i/dP_i per bus, volts per kW
	UseVoltage bool      // false until every bus has enough probes
}

// Stress mirrors feeder.Feeder.Stress using learned voltages.
func (n LearnedNetwork) Stress(inj []float64) float64 {
	s := 0.0
	run := 0.0
	flows := make([]float64, len(n.RatedKW))
	for k := len(n.RatedKW) - 1; k >= 0; k-- {
		if k < len(inj) {
			run += inj[k]
		}
		flows[k] = run
	}
	for k, r := range n.RatedKW {
		if a := math.Abs(flows[k]); a > r {
			s += a/r - 1
		}
	}
	if !n.UseVoltage {
		return s
	}
	for _, v := range (LearnedGate{NominalV: n.NominalV, Self: n.Self}).Voltages(inj) {
		pu := v / n.NominalV
		if hi := n.MaxPU - n.MarginPU; pu > hi {
			s += (pu - hi) * 100
		} else if lo := n.MinPU + n.MarginPU; pu < lo {
			s += (lo - pu) * 100
		}
	}
	return s
}

// PathResistance recovers cable resistance between two buses from the learned
// sensitivities: Self[b]-Self[a] is the resistance of the segments between them.
func (n LearnedNetwork) PathResistance(a, b int) float64 {
	if !n.UseVoltage {
		return 0
	}
	lo, hi := min(a, b), max(a, b)
	if hi >= len(n.Self) {
		return 0
	}
	return math.Max(0, (n.Self[hi]-n.Self[lo])*n.NominalV/1000)
}

// MaxError returns the largest voltage difference against another voltage vector.
func MaxError(a, b []float64) float64 {
	m := 0.0
	for i := range a {
		m = math.Max(m, math.Abs(a[i]-b[i]))
	}
	return m
}
