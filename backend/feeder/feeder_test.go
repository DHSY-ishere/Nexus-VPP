package feeder

import (
	"math"
	"testing"
)

func lv() *Feeder {
	f := &Feeder{Zone: "z", NominalV: 230, MinPU: 0.94, MaxPU: 1.06}
	for i := 0; i < 5; i++ {
		f.Segments = append(f.Segments, Segment{RatedKW: 20, ResistanceOhm: 0.05})
	}
	return f
}

func TestFlowsAreCumulativeFromTheFarEnd(t *testing.T) {
	flows := lv().Flows([]float64{1, 2, 3, 4, 5})
	want := []float64{15, 14, 12, 9, 5}
	for i := range want {
		if flows[i] != want[i] {
			t.Fatalf("flow %d = %v, want %v", i, flows[i], want[i])
		}
	}
}

// Voltage computed directly must equal the sensitivity matrix applied to the injections.
func TestSensitivityMatchesVoltages(t *testing.T) {
	f := lv()
	inj := []float64{2, -1, 3, -4, 5}
	v := f.Voltages(inj)
	for i := range v {
		want := f.NominalV
		for j, p := range inj {
			want += f.Sensitivity(i, j) * p
		}
		if math.Abs(v[i]-want) > 1e-9 {
			t.Fatalf("bus %d: voltage %v, sensitivity form %v", i, v[i], want)
		}
	}
}

func TestExportRaisesAndImportLowersVoltage(t *testing.T) {
	f := lv()
	up := f.Voltages([]float64{0, 0, 0, 0, 10})
	down := f.Voltages([]float64{0, 0, 0, 0, -10})
	if up[4] <= f.NominalV || down[4] >= f.NominalV {
		t.Fatalf("up %v down %v", up[4], down[4])
	}
}

func TestStressAndViolations(t *testing.T) {
	f := lv()
	if s := f.Stress(make([]float64, 5)); s != 0 {
		t.Fatalf("idle stress = %v", s)
	}
	inj := []float64{0, 0, 0, 0, -30} // 150% of rating on every segment
	if f.Stress(inj) <= 0 || len(f.Violations(inj)) == 0 {
		t.Fatal("expected violations")
	}
}
