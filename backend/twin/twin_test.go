package twin

import (
	"math"
	"math/rand"
	"testing"

	"github.com/DHSY-ishere/Nexus-VPP/backend/feeder"
)

func lv(n int) *feeder.Feeder {
	f := &feeder.Feeder{Zone: "z", NominalV: 230, MinPU: 0.94, MaxPU: 1.06}
	for i := 0; i < n; i++ {
		f.Segments = append(f.Segments, feeder.Segment{RatedKW: 20, ResistanceOhm: 0.03 + 0.01*float64(i)})
	}
	return f
}

func TestShapleyEfficiency(t *testing.T) {
	f := lv(6)
	// Evening-peak baseline that overloads the first segments.
	base := []float64{-6, -6, -6, -6, -6, -6}
	contribs := []Contribution{
		{"a", []Effect{{0, 8}, {3, -8}}}, {"b", []Effect{{1, 6}, {4, -6}}},
		{"c", []Effect{{3, -3}, {5, 3}}}, {"d", []Effect{{4, 5}, {2, -5}}}, {"e", []Effect{{5, 7}, {1, -7}}},
	}
	shares, total := ShapleyAttribution(f, base, contribs, 200, rand.New(rand.NewSource(1)))
	sum := 0.0
	for _, v := range shares {
		sum += v
	}
	if math.Abs(sum-total) > 1e-9 {
		t.Fatalf("shares sum %v != total %v", sum, total)
	}
}

func TestShapleyHelpfulBeatsHarmful(t *testing.T) {
	f := lv(6)
	base := []float64{-6, -6, -6, -6, -6, -6} // heavy import: injecting helps
	// Moving supply toward the heavily loaded far end helps; the reverse hurts.
	contribs := []Contribution{{"helper", []Effect{{5, 10}, {0, -10}}}, {"harmer", []Effect{{5, -10}, {0, 10}}}}
	shares, _ := ShapleyAttribution(f, base, contribs, 300, rand.New(rand.NewSource(2)))
	if shares["helper"] <= 0 || shares["harmer"] >= 0 {
		t.Fatalf("helper %v harmer %v", shares["helper"], shares["harmer"])
	}
}

// The model-free gate: learn each bus's self-sensitivity from noisy perturbation
// probes and rebuild voltages for an arbitrary injection pattern.
func TestLearnedGateMatchesModel(t *testing.T) {
	f := lv(8)
	rng := rand.New(rand.NewSource(3))
	g := LearnedGate{NominalV: f.NominalV, Self: make([]float64, f.Buses())}
	for i := range g.Self {
		var l SensitivityLearner
		for p := 0; p < 60; p++ {
			dP := 0.3 + rng.Float64()*0.5
			dV := f.Sensitivity(i, i)*dP + rng.NormFloat64()*0.03 // 30 mV meter noise
			l.Observe(dP, dV)
		}
		g.Self[i] = l.Estimate()
	}
	inj := []float64{3, -2, 4, -5, 1, 6, -3, 2}
	if e := MaxError(g.Voltages(inj), f.Voltages(inj)); e > 0.2 {
		t.Fatalf("learned gate voltage error %.3f V", e)
	}
}

// A LearnedNetwork built from learned sensitivities scores stress like the true
// feeder, and recovers path resistance without ever being given an impedance.
func TestLearnedNetworkMatchesFeeder(t *testing.T) {
	f := lv(8)
	rng := rand.New(rand.NewSource(5))
	n := LearnedNetwork{
		NominalV: f.NominalV, MinPU: f.MinPU, MaxPU: f.MaxPU,
		Self: make([]float64, f.Buses()), RatedKW: make([]float64, f.Buses()), UseVoltage: true,
	}
	for i, seg := range f.Segments {
		n.RatedKW[i] = seg.RatedKW
		var l SensitivityLearner
		for p := 0; p < 80; p++ {
			dP := 0.3 + rng.Float64()*0.5
			l.Observe(dP, f.Sensitivity(i, i)*dP+rng.NormFloat64()*0.03)
		}
		n.Self[i] = l.Estimate()
	}
	if got, want := n.PathResistance(2, 6), f.PathResistance(2, 6); math.Abs(got-want) > 0.004 {
		t.Fatalf("path resistance %v, want %v", got, want)
	}
	for _, inj := range [][]float64{
		{-6, -6, -6, -6, -6, -6, -6, -6}, // evening peak: loading and undervoltage
		{4, 4, 4, 4, 4, 4, 4, 4},         // midday export: overvoltage
		{0, 0, 0, 0, 0, 0, 0, 0},
	} {
		want := f.Stress(inj)
		if d := math.Abs(n.Stress(inj) - want); d > 0.03*math.Max(1, want) { // 3% relative
			t.Fatalf("stress differs by %.4f (true %.4f) for %v", d, want, inj)
		}
	}
	// Before enough probes, voltage is ignored and only loading counts.
	n.UseVoltage = false
	voltageOnly := []float64{2, 2, 2, 2, 2, 2, 2, 2} // 16 kW: inside cable ratings, over the voltage limit
	if f.Stress(voltageOnly) <= 0 {
		t.Fatal("test setup: this pattern should break the voltage limit on the true feeder")
	}
	if n.Stress(voltageOnly) != 0 {
		t.Fatal("thermal-only mode should not report voltage stress")
	}
}
