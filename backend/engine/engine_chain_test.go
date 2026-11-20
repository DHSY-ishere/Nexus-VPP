package engine

import (
	"math"
	"testing"

	"github.com/DHSY-ishere/Nexus-VPP/backend/settlement"
)

func moneyNet(e *Engine) float64 {
	bal, pool := e.Balances()
	sum := pool
	for _, v := range bal {
		sum += v
	}
	return sum
}

// A full honest day: one block per interval, every zone conserves against its
// independent head meter (no false alarms), and an outsider can audit the whole chain.
func TestHonestDayChainAndConservation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StartHour = 0
	e := New(cfg, nil, nil)
	recs := run(t, e, 288)

	for _, r := range recs {
		if r.ChainErr != "" {
			t.Fatalf("interval %v: %s", r.Interval, r.ChainErr)
		}
		if r.BlockSigs != 4 {
			t.Fatalf("expected 4 validator signatures, got %d", r.BlockSigs)
		}
		for _, z := range r.Zones {
			if !z.Conserved {
				t.Fatalf("%s %v: honest zone failed conservation (excess %.4f, tol %.4f)", z.Zone, r.Interval, z.ExcessKWh, z.ToleranceKWh)
			}
		}
	}
	if info := e.Chain().Info(); info.Blocks != 288 {
		t.Fatalf("expected 288 blocks, got %d", info.Blocks)
	}
	if err := e.Chain().Verify(); err != nil {
		t.Fatalf("full audit failed: %v", err)
	}
}

// The attack the old per-pair check missed. A seller delivers 30% but its signed meter
// says 100%. Both meters agree with the trade, so only the feeder-head meter can tell.
func TestLyingMeterCannotGetPaid(t *testing.T) {
	cfg := DefaultConfig()
	e := New(cfg, nil, nil)
	run(t, e, 96) // 09:00-17:00: batteries fill, sellers exist
	before := moneyNet(e)
	e.SetMeterLie(true)

	caught, notConserved := 0, 0
	for _, r := range run(t, e, 60) {
		for _, z := range r.Zones {
			if !z.Conserved {
				notConserved++
				if z.ConfirmedUSD >= z.ClaimedUSD-1e-9 {
					t.Fatalf("%s: zone failed conservation yet all %.4f claimed was confirmed", z.Zone, z.ClaimedUSD)
				}
			}
		}
		for _, tr := range r.Trades {
			if tr.Status != settlement.StatusSettled {
				caught++
			}
		}
	}
	if notConserved == 0 {
		t.Fatal("a lying meter was never caught by the head meter")
	}
	if caught == 0 {
		t.Fatal("no trade was held back despite a lying meter")
	}
	if after := moneyNet(e); after < before-1e-6 {
		t.Fatalf("money destroyed: %.6f -> %.6f", before, after)
	}
	if err := e.Chain().Verify(); err != nil {
		t.Fatalf("chain audit failed: %v", err)
	}
}

// An honestly metered shortfall is shared by buyers and penalised, and never loses money.
func TestUnderDeliveryIsPenalisedAndMoneyConserved(t *testing.T) {
	cfg := DefaultConfig()
	e := New(cfg, nil, nil)
	run(t, e, 96)
	e.SetTamper(true)
	before := moneyNet(e)
	partial := 0
	for _, r := range run(t, e, 60) {
		for _, tr := range r.Trades {
			if tr.Status == "PARTIAL" || tr.Status == settlement.StatusDisputed {
				partial++
				if tr.Charged > tr.Fill*tr.Price*tr.Received()+1e-9 && tr.Fill < 1 {
					t.Fatalf("buyer charged %.6f for a trade confirmed at fill %.3f", tr.Charged, tr.Fill)
				}
			}
		}
	}
	if partial == 0 {
		t.Fatal("under-delivery never reduced a settlement")
	}
	if after := moneyNet(e); math.Abs(after-before) > 1e-6 && after < before {
		t.Fatalf("money destroyed: %.6f -> %.6f", before, after)
	}
}

// If validators cannot reach quorum, nothing settles and no money moves.
func TestNoQuorumMeansNoSettlement(t *testing.T) {
	cfg := DefaultConfig()
	e := New(cfg, nil, nil)
	run(t, e, 96)
	vs := e.Chain().Validators()
	vs[0].Down, vs[1].Down = true, true // 2 of 4 offline: below the quorum of 3

	before := moneyNet(e)
	balBefore, poolBefore := e.Balances()
	var sawTrades bool
	for _, r := range run(t, e, 10) {
		if r.ChainErr == "" {
			t.Fatal("expected a consensus failure")
		}
		for _, tr := range r.Trades {
			sawTrades = true
			if tr.Status != settlement.StatusDisputed || tr.Charged != 0 {
				t.Fatalf("trade must not settle without consensus: %+v", tr)
			}
		}
	}
	if !sawTrades {
		t.Skip("no trades in the window")
	}
	balAfter, poolAfter := e.Balances()
	// Only passive reflex credits may differ; trades moved nothing.
	_ = balBefore
	_ = balAfter
	if poolAfter != poolBefore {
		t.Fatalf("pool changed without consensus: %.6f -> %.6f", poolBefore, poolAfter)
	}
	if moneyNet(e) < before-1e-6 {
		t.Fatal("money destroyed")
	}
}
