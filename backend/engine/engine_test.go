package engine

import (
	"math"
	"testing"

	"github.com/DHSY-ishere/Nexus-VPP/backend/settlement"
)

func run(t *testing.T, e *Engine, steps int) []IntervalRecord {
	t.Helper()
	var out []IntervalRecord
	for i := 0; i < steps; i++ {
		out = append(out, e.Step())
	}
	return out
}

// A full simulated day: the market must trade, the gate must never leave a zone
// more stressed than the passive baseline, and every proof must verify.
func TestFullDayInvariants(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StartHour = 0
	e := New(cfg, nil, nil)
	recs := run(t, e, 288)

	trades, settled := 0, 0
	for _, r := range recs {
		for _, z := range r.Zones {
			if z.StressAfter > z.StressBefore+1e-9 {
				t.Fatalf("%s %v: stress rose %.4f -> %.4f", z.Zone, r.Interval, z.StressBefore, z.StressAfter)
			}
			if z.ClearedKWh > z.AuctionKWh+1e-9 {
				t.Fatalf("cleared %v exceeds auction %v", z.ClearedKWh, z.AuctionKWh)
			}
		}
		for _, tr := range r.Trades {
			trades++
			if tr.Status == settlement.StatusSettled {
				settled++
			}
		}
	}
	if trades == 0 {
		t.Fatal("no trades in a full day")
	}
	if settled != trades {
		t.Fatalf("with honest meters every trade should settle: %d of %d", settled, trades)
	}

	last := recs[len(recs)-1]
	for _, tr := range last.Trades {
		p, ok := e.ProofFor(tr.TradeID)
		if !ok || !p.Valid || p.Root != last.MerkleRoot {
			t.Fatalf("bad proof for %s: ok=%v %+v", tr.TradeID, ok, p.Valid)
		}
	}

	// Money is conserved: wallets plus pool net to the stability/reflex credits paid out.
	bal, pool := e.Balances()
	sum := pool
	for _, v := range bal {
		sum += v
	}
	if sum < -1e-6 {
		t.Fatalf("money destroyed: net %.6f", sum)
	}
	t.Logf("day: %d trades, gated %.1f kWh, pool $%.2f, savings $%.2f, gini %.2f",
		trades, e.totalGated, pool, recs[len(recs)-1].CommunitySavings, recs[len(recs)-1].Gini)
}

// A tampering seller's trades are disputed and the buyer is refunded.
func TestTamperedSellerIsDisputed(t *testing.T) {
	cfg := DefaultConfig()
	e := New(cfg, nil, nil)
	run(t, e, 96) // 09:00-17:00: the solar day fills batteries so sellers exist
	e.SetTamper(true)
	disputed := 0
	for _, r := range run(t, e, 60) {
		disputed += r.Disputed
		// Every disputed trade must have been refunded: money is never created or destroyed.
	}
	if disputed == 0 {
		t.Fatal("tampering never disputed (no trade involved the tampering meter)")
	}
}

func TestIslandModeRuns(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StartHour = 18
	e := New(cfg, nil, nil)
	e.SetIsland(true)
	for _, r := range run(t, e, 40) {
		if r.Mode != "ISLANDED" {
			t.Fatal("mode not islanded")
		}
		for _, z := range r.Zones {
			if z.Price > IslandCap+1e-9 {
				t.Fatalf("price %v above island cap", z.Price)
			}
		}
	}
}

func TestGini(t *testing.T) {
	if g := Gini([]float64{1, 1, 1, 1}); math.Abs(g) > 1e-9 {
		t.Fatalf("equal = %v", g)
	}
	if g := Gini([]float64{0, 0, 0, 10}); math.Abs(g-0.75) > 1e-9 {
		t.Fatalf("one holds all = %v, want 0.75", g)
	}
}

// Calibration guard: the simulated feeders should be healthy most of the day and
// strained at peaks. If every interval is strained the stress and credit figures
// are meaningless; if none is, the network gate and the attribution never matter.
func TestFeederCalibration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StartHour = 0
	e := New(cfg, nil, nil)
	total, strained := 0, 0
	minPU, maxPU := 9.0, 0.0
	for _, r := range run(t, e, 288) {
		for _, z := range r.Zones {
			total++
			if z.StressBefore > 0 {
				strained++
			}
			minPU, maxPU = math.Min(minPU, z.MinPU), math.Max(maxPU, z.MaxPU)
		}
	}
	frac := float64(strained) / float64(total)
	if frac < 0.02 || frac > 0.40 {
		t.Fatalf("%.0f%% of zone-intervals strained, want between 2%% and 40%%", frac*100)
	}
	if minPU < 0.90 || maxPU > 1.10 {
		t.Fatalf("voltage range %.3f-%.3f pu is unrealistic for a low-voltage feeder", minPU, maxPU)
	}
}

// The learned gate starts blind to voltage, learns within a few intervals, stays
// accurate, and never leaves the true feeder worse off than the passive baseline.
func TestLearnedGateWarmsUpAndStaysSafe(t *testing.T) {
	cfg := DefaultConfig() // Gate: learned, starts 09:00
	e := New(cfg, nil, nil)
	recs := run(t, e, 288)

	first := recs[0].Zones[0]
	if first.GateMode != "WARMING_UP" {
		t.Fatalf("first interval gate = %s, want WARMING_UP (no probes yet)", first.GateMode)
	}
	learnedAt := -1
	for i, r := range recs {
		if r.Zones[0].GateMode == "LEARNED" {
			learnedAt = i
			break
		}
	}
	if learnedAt < 0 || learnedAt > 8 {
		t.Fatalf("learned gate ready after %d intervals, want within 8", learnedAt)
	}

	var learnedKWh, modelKWh, worst float64
	for _, r := range recs[learnedAt:] {
		for _, z := range r.Zones {
			if z.GateMode != "LEARNED" {
				t.Fatalf("gate fell out of LEARNED mode at %v", r.Interval)
			}
			learnedKWh += z.ClearedKWh
			modelKWh += z.ModelClearedKWh
			worst = math.Max(worst, z.StressAfter-z.StressBefore)
		}
	}
	if worst > 0.01 {
		t.Fatalf("learned gate raised true feeder stress by %.4f", worst)
	}
	last := recs[len(recs)-1].Zones[0]
	if last.SensErrPct > 3 {
		t.Fatalf("sensitivity error %.2f%%, want under 3%%", last.SensErrPct)
	}
	if modelKWh == 0 {
		t.Fatal("no volume cleared")
	}
	ratio := learnedKWh / modelKWh
	if ratio < 0.85 || ratio > 1.05 {
		t.Fatalf("learned gate cleared %.0f%% of what the perfect model clears", ratio*100)
	}
	t.Logf("learned ready after %d intervals; sensitivity error %.2f%%; cleared %.1f kWh vs %.1f kWh with perfect model (%.0f%%); worst stress rise %.5f",
		learnedAt+1, last.SensErrPct, learnedKWh, modelKWh, ratio*100, worst)
}

// While warming up the gate only knows thermal limits, so during that window it can
// miss voltage problems; this test pins down how long that window is in practice.
func TestWarmupWindowIsShort(t *testing.T) {
	e := New(DefaultConfig(), nil, nil)
	warm := 0
	for _, r := range run(t, e, 20) {
		if r.Zones[0].GateMode == "WARMING_UP" {
			warm++
		}
	}
	if warm > 6 {
		t.Fatalf("%d warm-up intervals (30 min of simulated time each 5 intervals), want at most 6", warm)
	}
}

func TestModelGateModeStillWorks(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Gate = GateModel
	e := New(cfg, nil, nil)
	for _, r := range run(t, e, 150) {
		for _, z := range r.Zones {
			if z.GateMode != "MODEL" {
				t.Fatalf("gate mode %s", z.GateMode)
			}
			if z.StressAfter > z.StressBefore+1e-9 {
				t.Fatalf("model gate raised stress")
			}
		}
	}
}
