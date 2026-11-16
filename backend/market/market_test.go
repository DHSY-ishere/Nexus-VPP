package market

import (
	"math"
	"testing"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/feeder"
)

func testFeeder(n int, rated, r float64) *feeder.Feeder {
	f := &feeder.Feeder{Zone: "z1", NominalV: 230, MinPU: 0.94, MaxPU: 1.06}
	for i := 0; i < n; i++ {
		f.Segments = append(f.Segments, feeder.Segment{RatedKW: rated, ResistanceOhm: r})
	}
	return f
}

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
var opt = Options{Interval: 5 * time.Minute}

func sell(id string, bus int, kwh, price float64) Bid {
	return Bid{AgentID: id, Zone: "z1", Bus: bus, Side: Sell, Steps: []Step{{kwh, price}}}
}
func buy(id string, bus int, kwh, price float64) Bid {
	return Bid{AgentID: id, Zone: "z1", Bus: bus, Side: Buy, Steps: []Step{{kwh, price}}}
}

func TestUniformPriceAndVolume(t *testing.T) {
	f := testFeeder(4, 100, 0.01)
	base := make([]float64, 4)
	bids := []Bid{sell("s1", 0, 1, 0.10), sell("s2", 1, 1, 0.14), buy("b1", 2, 1.5, 0.20), buy("b2", 3, 1, 0.12)}
	r := ClearZone(f, base, bids, t0, opt)
	// b1 takes 1 kWh from s1 and 0.5 from s2. b2 (0.12) is below s2's ask (0.14).
	if math.Abs(r.AuctionKWh-1.5) > 1e-9 {
		t.Fatalf("auction volume = %v, want 1.5", r.AuctionKWh)
	}
	if math.Abs(r.Price-0.17) > 1e-9 { // midpoint of marginal pair (0.14, 0.20)
		t.Fatalf("price = %v, want 0.17", r.Price)
	}
	for _, tr := range r.Trades {
		if tr.Price != r.Price {
			t.Fatalf("non-uniform price %v vs %v", tr.Price, r.Price)
		}
		if tr.BuyerID == "b2" {
			t.Fatalf("b2 should not trade")
		}
	}
}

func TestNoCrossNoTrade(t *testing.T) {
	f := testFeeder(3, 100, 0.01)
	r := ClearZone(f, make([]float64, 3), []Bid{sell("s", 0, 1, 0.30), buy("b", 1, 1, 0.10)}, t0, opt)
	if len(r.Trades) != 0 || r.AuctionKWh != 0 {
		t.Fatalf("expected no trades, got %+v", r)
	}
}

// A heavily loaded far end cannot absorb extra charging, so the gate cuts the trade.
func TestGateCurtailsOverload(t *testing.T) {
	f := testFeeder(3, 10, 0.001)
	base := []float64{0, 0, -9.5} // segment 2 already at 95% of rating
	r := ClearZone(f, base, []Bid{sell("s", 0, 2, 0.05), buy("b", 2, 2, 0.30)}, t0, opt)
	if r.AuctionKWh != 2 {
		t.Fatalf("auction volume = %v", r.AuctionKWh)
	}
	if r.ClearedKWh >= r.AuctionKWh {
		t.Fatalf("gate did not cut the trade: cleared %v", r.ClearedKWh)
	}
	if r.StressAfter > r.StressBefore+1e-9 {
		t.Fatalf("stress rose: %v -> %v", r.StressBefore, r.StressAfter)
	}
	if r.GatedKWh <= 0 {
		t.Fatal("GatedKWh should be positive")
	}
}

// The gate must never leave the feeder worse than it found it.
func TestGateNeverWorsensStress(t *testing.T) {
	f := testFeeder(6, 12, 0.05)
	base := []float64{-3, -2, -2, -4, -6, -8}
	bids := []Bid{sell("a", 0, 3, 0.05), sell("b", 1, 3, 0.06), buy("c", 4, 3, 0.4), buy("d", 5, 3, 0.4)}
	r := ClearZone(f, base, bids, t0, opt)
	if r.StressAfter > r.StressBefore+1e-9 {
		t.Fatalf("stress rose: %v -> %v", r.StressBefore, r.StressAfter)
	}
}

func TestNearestSellerFirst(t *testing.T) {
	f := testFeeder(6, 100, 0.01)
	bids := []Bid{sell("far", 0, 1, 0.05), sell("near", 4, 1, 0.05), buy("b", 5, 1, 0.3)}
	r := ClearZone(f, make([]float64, 6), bids, t0, opt)
	if len(r.Trades) != 1 || r.Trades[0].SellerID != "near" {
		t.Fatalf("expected near seller, got %+v", r.Trades)
	}
	if r.Trades[0].GateResult != GatePass {
		t.Fatalf("gate = %s", r.Trades[0].GateResult)
	}
}

func TestIslandPriority(t *testing.T) {
	f := testFeeder(4, 100, 0.01)
	critical := buy("crit", 3, 1, 0.01) // price far below the ask
	critical.Priority = 2
	bids := []Bid{sell("s", 0, 1, 0.20), buy("rich", 2, 1, 0.90), critical}
	r := ClearZone(f, make([]float64, 4), bids, t0, Options{Interval: 5 * time.Minute, Island: true, PriceCap: 1})
	if len(r.Trades) != 1 || r.Trades[0].BuyerID != "crit" {
		t.Fatalf("critical load should win under scarcity, got %+v", r.Trades)
	}
}

func TestEnergyBalance(t *testing.T) {
	f := testFeeder(5, 100, 0.01)
	bids := []Bid{sell("a", 0, 2, 0.05), sell("b", 1, 1, 0.07), buy("c", 3, 2, 0.3), buy("d", 4, 1.5, 0.25)}
	r := ClearZone(f, make([]float64, 5), bids, t0, opt)
	sum := 0.0
	for _, tr := range r.Trades {
		sum += tr.EnergyKWh
	}
	if math.Abs(sum-r.ClearedKWh) > 1e-9 {
		t.Fatalf("trade sum %v != cleared %v", sum, r.ClearedKWh)
	}
	net := 0.0
	for _, v := range r.Injections { // baseline is zero, so this is the trades' net injection
		net += v
	}
	if math.Abs(net) > 1e-6 {
		t.Fatalf("trades are not energy-neutral: %v", net)
	}
}
