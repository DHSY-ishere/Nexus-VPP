package agent

import (
	"testing"

	"github.com/DHSY-ishere/Nexus-VPP/backend/market"
)

// At 12:00 the target state of charge is 62%. With the 6-point band a home sells above 68%
// and buys below 56%. A demand-response level lowers the target by 8 points per level.

func TestDemandResponseTurnsAHomeIntoASeller(t *testing.T) {
	a := home(58) // neutral at level 0: between 56 and 68
	if _, ok := a.Bid(12, 5.0/60, false); ok {
		t.Fatal("setup: a 58% home should not bid at level 0")
	}
	a.DRLevel = 2 // target 46%, so it sells above 52%
	bid, ok := a.Bid(12, 5.0/60, false)
	if !ok || bid.Side != market.Sell {
		t.Fatalf("DR level 2 should make a 58%% home a seller, got %+v ok=%v", bid, ok)
	}
}

func TestDemandResponseShedsDemand(t *testing.T) {
	a := home(50) // a buyer at level 0 (below 56)
	if bid, ok := a.Bid(12, 5.0/60, false); !ok || bid.Side != market.Buy {
		t.Fatalf("setup: a 50%% home should buy at level 0, got %+v", bid)
	}
	a.DRLevel = 2 // target 46%, it buys only below 40%
	if bid, ok := a.Bid(12, 5.0/60, false); ok {
		t.Fatalf("DR level 2 should shed this home's demand, got %+v", bid)
	}
}

func TestDemandResponseNeverBreaksTheOwnersReserve(t *testing.T) {
	a := home(30)
	a.DRLevel = 3
	if bid, ok := a.Bid(12, 5.0/60, false); ok && bid.Side == market.Sell {
		t.Fatalf("a home at 30%% must not sell below its 25%% reserve plus the sale: %+v", bid)
	}
	a = home(60)
	a.DRLevel = 3
	bid, ok := a.Bid(12, 5.0/60, false)
	if !ok || bid.Side != market.Sell {
		t.Fatalf("expected a seller, got %+v", bid)
	}
	total := 0.0
	for _, s := range bid.Steps {
		total += s.KWh
	}
	if left := a.SoC - total/a.CapKWh*100; left < a.Limits.ReserveSoC-1e-9 {
		t.Fatalf("DR drove the home to %.1f%%, below its %.0f%% reserve", left, a.Limits.ReserveSoC)
	}
}

func TestPriceSignalCapsBuyerBids(t *testing.T) {
	free, capped := home(40), home(40)
	capped.DRCeiling = 0.10
	bf, _ := free.Bid(12, 5.0/60, false)
	bc, _ := capped.Bid(12, 5.0/60, false)
	if bf.Steps[0].Price != 0.28 {
		t.Fatalf("uncapped ceiling = %v", bf.Steps[0].Price)
	}
	for _, s := range bc.Steps {
		if s.Price > 0.10+1e-12 {
			t.Fatalf("capped bid step priced %v above the 0.10 signal", s.Price)
		}
	}
	// A signal above the owner's own ceiling changes nothing: owners are never made to pay more.
	high := home(40)
	high.DRCeiling = 0.90
	if bh, _ := high.Bid(12, 5.0/60, false); bh.Steps[0].Price != 0.28 {
		t.Fatalf("a high signal must not raise the owner's ceiling, got %v", bh.Steps[0].Price)
	}
}
