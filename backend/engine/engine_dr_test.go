package engine

import "testing"

func TestDemandResponseReachesAgentsAndResets(t *testing.T) {
	e := New(DefaultConfig(), nil, nil)
	run(t, e, 4)
	e.SetDR(3, 0.10)
	r := run(t, e, 1)[0]
	if r.DRLevel != 3 || r.DRPrice != 0.10 {
		t.Fatalf("record shows DR %d / %.2f", r.DRLevel, r.DRPrice)
	}
	if h := e.homes[0][0]; h.DRLevel != 3 || h.DRCeiling != 0.10 {
		t.Fatalf("agent not updated: level %d ceiling %.2f", h.DRLevel, h.DRCeiling)
	}
	e.ClearFaults()
	if r := run(t, e, 1)[0]; r.DRLevel != 0 || r.DRPrice != 0 {
		t.Fatalf("ClearFaults left DR active: %d / %.2f", r.DRLevel, r.DRPrice)
	}
}

// A price signal caps every buyer's bid, and the auction's uniform price can never exceed
// the marginal buyer's bid, so no trade may clear above the signal.
func TestPriceSignalBoundsClearingPrices(t *testing.T) {
	cfg := DefaultConfig()
	e := New(cfg, nil, nil)
	run(t, e, 96) // build up batteries so there is something to trade
	e.SetDR(0, 0.12)
	trades := 0
	for _, r := range run(t, e, 120) {
		for _, tr := range r.Trades {
			trades++
			if tr.Price > 0.12+1e-9 {
				t.Fatalf("trade cleared at %.4f above the 0.12 price signal", tr.Price)
			}
		}
	}
	t.Logf("%d trades cleared under the price signal", trades)
}
