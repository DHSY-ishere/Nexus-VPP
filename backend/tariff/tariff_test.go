package tariff

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// The Python implementation (ml/tariff.py) is tested against the same vectors.
func TestMatchesSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("../../ml/tariff_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Min, Max, TriggerHz, FloorHz float64 `json:"-"`
		MinV                         float64 `json:"min"`
		MaxV                         float64 `json:"max"`
		Trig                         float64 `json:"trigger_hz"`
		Floor                        float64 `json:"floor_hz"`
		Cases                        []struct{ Freq, Price float64 }
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.MinV != Min || v.MaxV != Max || v.Trig != TriggerHz || v.Floor != FloorHz {
		t.Fatalf("constants differ from shared vectors: %+v", v)
	}
	for _, c := range v.Cases {
		if got := Price(c.Freq); math.Abs(got-c.Price) > 1e-9 {
			t.Fatalf("Price(%.1f) = %.6f, want %.6f", c.Freq, got, c.Price)
		}
	}
}

func TestBoundsAndMonotonic(t *testing.T) {
	prev := -1.0
	for f := 50.5; f >= 47.0; f -= 0.01 {
		p := Price(f)
		if p < Min-1e-12 || p > Max+1e-12 {
			t.Fatalf("price %.4f at %.2f Hz outside [%.2f, %.2f]", p, f, Min, Max)
		}
		if p < prev-1e-12 {
			t.Fatalf("price fell as strain rose at %.2f Hz", f)
		}
		prev = p
	}
	if math.Abs(Price(49.0)-Max) > 1e-9 || math.Abs(Price(49.8)-Min) > 1e-9 {
		t.Fatalf("endpoints: %.3f %.3f", Price(49.0), Price(49.8))
	}
}

func TestCredit(t *testing.T) {
	if got := Credit(10, 49.0); math.Abs(got-8.5) > 1e-9 {
		t.Fatalf("Credit = %v", got)
	}
}
