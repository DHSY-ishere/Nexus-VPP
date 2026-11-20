package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/delivery"
	"github.com/DHSY-ishere/Nexus-VPP/backend/feeder"
	"github.com/DHSY-ishere/Nexus-VPP/backend/market"
	"github.com/DHSY-ishere/Nexus-VPP/backend/settlement"
)

const hrs = 5.0 / 60

type rig struct {
	f    *feeder.Feeder
	reg  *settlement.Registry
	keys map[string]ed25519.PrivateKey
	c    *Chain
}

func newRig(t *testing.T, retain int) *rig {
	f := &feeder.Feeder{Zone: "Z1", NominalV: 230, MinPU: 0.94, MaxPU: 1.06}
	for i := 0; i < 6; i++ {
		f.Segments = append(f.Segments, feeder.Segment{RatedKW: 50, ResistanceOhm: 0.02})
	}
	r := &rig{f: f, reg: settlement.NewRegistry(), keys: map[string]ed25519.PrivateKey{}}
	for _, id := range []string{"H0", "H1", "H2", "H3", "H4", "H5", delivery.HeadID("Z1")} {
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		r.reg.Enroll(id, pub)
		r.keys[id] = priv
	}
	r.c = New(4, Oracle{Reg: r.reg, Feeders: map[string]*feeder.Feeder{"Z1": f}}, retain)
	return r
}

// zone builds an honest zone record for interval k with one trade H0 -> H2.
func (r *rig) zone(k int, deliveredKWh float64) ZoneRecord {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(k) * 5 * time.Minute)
	tr := market.Trade{TradeID: fmt.Sprintf("T%03d", k), Zone: "Z1", IntervalStart: start,
		SellerID: "H0", BuyerID: "H2", EnergyKWh: 1, Price: 0.15, LossFactor: 0.01, GateResult: market.GatePass}
	in := delivery.Input{Zone: "Z1", Start: start, Hours: hrs, Trades: []market.Trade{tr}}
	net := make([]float64, 6)
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("H%d", i)
		v := 0.1
		if id == "H0" {
			v += deliveredKWh
		}
		if id == "H2" {
			v -= 1
		}
		net[i] = v
		in.Homes = append(in.Homes, delivery.Home{ID: id, Bus: i, BaselineKWh: 0.1,
			Reading: settlement.SignReading(r.keys[id], id, start, v)})
	}
	var sum float64
	for _, v := range net {
		sum += v
	}
	in.Head = settlement.SignReading(r.keys[delivery.HeadID("Z1")], delivery.HeadID("Z1"), start,
		sum-delivery.LossKWh(r.f, net, hrs))
	return ZoneRecord{Input: in, Result: delivery.Settle(in, r.reg, r.f)}
}

func (r *rig) commit(t *testing.T, k int) Block {
	t.Helper()
	z := r.zone(k, 1)
	b, err := r.c.Propose(z.Input.Start, []ZoneRecord{z})
	if err != nil {
		t.Fatalf("block %d: %v", k, err)
	}
	return b
}

func TestHonestBlocksCommitAndVerify(t *testing.T) {
	r := newRig(t, 0)
	for k := 0; k < 12; k++ {
		b := r.commit(t, k)
		if len(b.Sigs) != 4 {
			t.Fatalf("block %d: %d sigs, want 4", k, len(b.Sigs))
		}
	}
	if err := r.c.Verify(); err != nil {
		t.Fatal(err)
	}
	if info := r.c.Info(); info.Quorum != 3 || info.Blocks != 12 || info.Height != 12 {
		t.Fatalf("unexpected info %+v", info)
	}
}

func TestProposerRotates(t *testing.T) {
	r := newRig(t, 0)
	seen := map[string]bool{}
	for k := 0; k < 8; k++ {
		seen[r.commit(t, k).Proposer] = true
	}
	if len(seen) != 4 {
		t.Fatalf("all four validators should propose in turn, saw %v", seen)
	}
}

// A malicious proposer awards a seller more money than the rules allow. Honest
// validators re-execute, disagree, and refuse to sign. One colluding validator is not
// enough: 2 signatures of 4 is below the quorum of 3.
func TestForgedSettlementIsRejected(t *testing.T) {
	r := newRig(t, 0)
	r.commit(t, 0)
	r.c.Validators()[1].Faulty = true // a Byzantine validator signs anything

	z := r.zone(1, 0.0)                                // the seller delivered nothing...
	z.Result.Trades[0].Status = delivery.StatusSettled // ...but the proposer claims full settlement
	z.Result.Trades[0].Charge = z.Result.Trades[0].Escrow
	z.Result.Trades[0].Fill = 1
	_, err := r.c.Propose(z.Input.Start, []ZoneRecord{z})
	if err == nil {
		t.Fatal("forged settlement must not reach quorum")
	}
	if got := r.c.Info().Blocks; got != 1 {
		t.Fatalf("rejected block must not be appended, chain has %d blocks", got)
	}
}

// Rewriting history after the fact is detected by a full audit.
func TestEditingHistoryIsDetected(t *testing.T) {
	edits := map[string]func(b *Block){
		"change a payout":        func(b *Block) { b.Zones[0].Result.Trades[0].Charge += 0.05 },
		"change a trade":         func(b *Block) { b.Zones[0].Input.Trades[0].EnergyKWh += 1 },
		"change a meter reading": func(b *Block) { b.Zones[0].Input.Homes[0].Reading.NetKWh += 0.5 },
		"change the head meter":  func(b *Block) { b.Zones[0].Input.Head.NetKWh -= 0.5 },
		"drop a signature":       func(b *Block) { b.Sigs = b.Sigs[:2] },
		"relink the chain":       func(b *Block) { b.PrevHash = "0000" },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 0)
			for k := 0; k < 6; k++ {
				r.commit(t, k)
			}
			if err := r.c.Verify(); err != nil {
				t.Fatalf("clean chain should verify: %v", err)
			}
			edit(&r.c.blocks[3])
			if err := r.c.Verify(); err == nil {
				t.Fatal("edit went undetected")
			}
		})
	}
}

// Even an attacker who recomputes the block hash cannot forge the outcome, because
// re-execution of the rules gives a different answer.
func TestRehashedForgeryStillFails(t *testing.T) {
	r := newRig(t, 0)
	for k := 0; k < 4; k++ {
		r.commit(t, k)
	}
	b := &r.c.blocks[2]
	b.Zones[0].Result.Trades[0].Charge += 0.05
	b.TradeRoot = TradeRoot(b.Zones)
	b.Hash = HashOf(*b)
	if err := r.c.Verify(); err == nil {
		t.Fatal("rehashed forgery must fail re-execution or signature checks")
	}
}

func TestToleratesOneCrashedValidatorButNotTwo(t *testing.T) {
	r := newRig(t, 0)
	r.c.Validators()[2].Down = true
	if b := r.commit(t, 0); len(b.Sigs) != 3 {
		t.Fatalf("3 of 4 should still finalise, got %d sigs", len(b.Sigs))
	}
	r.c.Validators()[3].Down = true
	z := r.zone(1, 1)
	if _, err := r.c.Propose(z.Input.Start, []ZoneRecord{z}); err == nil {
		t.Fatal("2 of 4 must not finalise")
	}
	if err := r.c.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestPruningKeepsChainVerifiable(t *testing.T) {
	r := newRig(t, 5)
	for k := 0; k < 20; k++ {
		r.commit(t, k)
	}
	info := r.c.Info()
	if info.Blocks != 5 || info.Height != 20 {
		t.Fatalf("expected 5 retained blocks and height 20, got %+v", info)
	}
	if err := r.c.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestFindTradeAndTradeRoot(t *testing.T) {
	r := newRig(t, 0)
	for k := 0; k < 4; k++ {
		r.commit(t, k)
	}
	b, z, ok := r.c.FindTrade("T002")
	if !ok || b.Height != 2 || z.Input.Trades[0].TradeID != "T002" {
		t.Fatalf("FindTrade failed: ok=%v height=%d", ok, b.Height)
	}
	if TradeRoot(b.Zones) != b.TradeRoot {
		t.Fatal("trade root mismatch")
	}
	if _, _, ok := r.c.FindTrade("nope"); ok {
		t.Fatal("unknown trade should not be found")
	}
}
