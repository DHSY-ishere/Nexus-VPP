package agent

import (
	"testing"

	"github.com/DHSY-ishere/Nexus-VPP/backend/market"
)

func home(soc float64) *Agent {
	return &Agent{ID: "h", Zone: "z", Bus: 1, CapKWh: 10, MaxKW: 5, SoC: soc,
		Limits: Limits{ReserveSoC: 25, MinSellPrice: 0.08, MaxBuyPrice: 0.28}, WearCostPerKWh: 0.03}
}

func TestSellerNeverGoesBelowReserve(t *testing.T) {
	a := home(95)
	bid, ok := a.Bid(17, 5.0/60, false)
	if !ok || bid.Side != market.Sell {
		t.Fatalf("expected a sell bid, got %+v ok=%v", bid, ok)
	}
	total := 0.0
	for _, s := range bid.Steps {
		total += s.KWh
		if s.Price < a.Limits.MinSellPrice {
			t.Fatalf("step priced %v below owner's minimum", s.Price)
		}
	}
	if left := a.SoC - total/a.CapKWh*100; left < a.Limits.ReserveSoC {
		t.Fatalf("selling %v kWh would leave %v%% (< reserve)", total, left)
	}
}

func TestBuyerNeverPaysAboveCeiling(t *testing.T) {
	a := home(30)
	bid, ok := a.Bid(17, 5.0/60, false)
	if !ok || bid.Side != market.Buy {
		t.Fatalf("expected a buy bid, got %+v ok=%v", bid, ok)
	}
	for _, s := range bid.Steps {
		if s.Price > a.Limits.MaxBuyPrice+1e-12 {
			t.Fatalf("step %v above ceiling", s.Price)
		}
	}
}

func TestIslandCriticalGetsPriority(t *testing.T) {
	a := home(20)
	a.Limits.Critical = true
	bid, ok := a.Bid(19, 5.0/60, true)
	if !ok || bid.Priority != 2 {
		t.Fatalf("critical agent should bid with priority 2: %+v ok=%v", bid, ok)
	}
}

func TestReflex(t *testing.T) {
	a := home(80)
	a.Reflex = &Reflex{HzBelow: 49.8, KWPerHz: 10, VoltBelow: 220, KWPerVolt: 0.2}
	if kw := a.Respond(50.0, 230); kw != 0 {
		t.Fatalf("nominal grid should not trigger reflex, got %v", kw)
	}
	if kw := a.Respond(49.3, 230); kw <= 0 || kw > a.MaxKW {
		t.Fatalf("under-frequency response = %v", kw)
	}
	a.SoC = 20 // below reserve
	if kw := a.Respond(49.3, 230); kw != 0 {
		t.Fatalf("reflex must respect reserve, got %v", kw)
	}
}
