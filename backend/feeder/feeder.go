// Package feeder models one low-voltage radial feeder (L1 physical network / L6 twin).
//
// Topology: transformer -> segment 0 -> bus 0 -> segment 1 -> bus 1 -> ... -> bus n-1.
// Segment k therefore carries the net power of every bus at or beyond k.
//
// Sign convention for injections: kW, positive = the bus pushes power into the
// feeder (generation / battery discharge), negative = the bus draws power (load /
// battery charge). Flow on a segment is positive when it points toward the
// transformer.
//
// This is a linearised (DC-style) model: voltage change along a segment is
// R * I with I = P/V0. It is deliberately simple. It is accurate enough for a
// low-voltage feeder with small voltage deviations and keeps the market gate fast.
package feeder

import "math"

// Segment is one cable section.
type Segment struct {
	RatedKW       float64 // thermal limit, applied to |flow|
	ResistanceOhm float64
}

// Feeder is a single market zone.
type Feeder struct {
	Zone     string
	NominalV float64
	MinPU    float64 // lower voltage limit, per unit of NominalV
	MaxPU    float64 // upper voltage limit
	Segments []Segment
}

// Violation describes one broken limit.
type Violation struct {
	Kind  string  `json:"kind"` // "LOADING", "OVERVOLTAGE", "UNDERVOLTAGE"
	Index int     `json:"index"`
	Value float64 `json:"value"`
	Limit float64 `json:"limit"`
}

// Buses returns the number of customer buses.
func (f *Feeder) Buses() int { return len(f.Segments) }

// Flows returns the flow (kW) on each segment for the given bus injections.
func (f *Feeder) Flows(inj []float64) []float64 {
	n := len(f.Segments)
	out := make([]float64, n)
	run := 0.0
	for k := n - 1; k >= 0; k-- {
		if k < len(inj) {
			run += inj[k]
		}
		out[k] = run
	}
	return out
}

// Voltages returns the voltage (V) at each bus.
func (f *Feeder) Voltages(inj []float64) []float64 {
	flows := f.Flows(inj)
	v := make([]float64, len(f.Segments))
	rise := 0.0
	for k, s := range f.Segments {
		rise += s.ResistanceOhm * flows[k] * 1000 / f.NominalV
		v[k] = f.NominalV + rise
	}
	return v
}

// Sensitivity returns dV_i/dP_j in volts per kW. On a radial feeder the shared
// electrical path of buses i and j ends at the bus closer to the transformer, so
// it depends only on min(i, j).
func (f *Feeder) Sensitivity(i, j int) float64 {
	m := min(i, j)
	sum := 0.0
	for k := 0; k <= m && k < len(f.Segments); k++ {
		sum += f.Segments[k].ResistanceOhm
	}
	return sum * 1000 / f.NominalV
}

// PathResistance returns the resistance of the cable between two buses.
func (f *Feeder) PathResistance(a, b int) float64 {
	lo, hi := min(a, b), max(a, b)
	sum := 0.0
	for k := lo + 1; k <= hi && k < len(f.Segments); k++ {
		sum += f.Segments[k].ResistanceOhm
	}
	return sum
}

// Violations lists every broken limit for the given injections.
func (f *Feeder) Violations(inj []float64) []Violation {
	var out []Violation
	flows := f.Flows(inj)
	for k, s := range f.Segments {
		if a := math.Abs(flows[k]); a > s.RatedKW {
			out = append(out, Violation{"LOADING", k, a, s.RatedKW})
		}
	}
	for i, v := range f.Voltages(inj) {
		hi, lo := f.MaxPU*f.NominalV, f.MinPU*f.NominalV
		if v > hi {
			out = append(out, Violation{"OVERVOLTAGE", i, v, hi})
		} else if v < lo {
			out = append(out, Violation{"UNDERVOLTAGE", i, v, lo})
		}
	}
	return out
}

// Stress is a single scalar of how badly the feeder is outside its limits; 0 means
// all limits hold. Loading excess counts as a fraction of rating, voltage excess as
// percentage points of nominal voltage. It is convex in the injections, which the
// market gate relies on for its bisection.
func (f *Feeder) Stress(inj []float64) float64 {
	s := 0.0
	flows := f.Flows(inj)
	for k, seg := range f.Segments {
		if a := math.Abs(flows[k]); a > seg.RatedKW {
			s += a/seg.RatedKW - 1
		}
	}
	for _, v := range f.Voltages(inj) {
		pu := v / f.NominalV
		if pu > f.MaxPU {
			s += (pu - f.MaxPU) * 100
		} else if pu < f.MinPU {
			s += (f.MinPU - pu) * 100
		}
	}
	return s
}
