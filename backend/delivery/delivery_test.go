package delivery

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/feeder"
	"github.com/DHSY-ishere/Nexus-VPP/backend/market"
	"github.com/DHSY-ishere/Nexus-VPP/backend/settlement"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

const hrs = 5.0 / 60

type world struct {
	f    *feeder.Feeder
	reg  *settlement.Registry
	keys map[string]ed25519.PrivateKey
	n    int
}

func newWorld(n int) *world {
	f := &feeder.Feeder{Zone: "Z1", NominalV: 230, MinPU: 0.94, MaxPU: 1.06}
	for i := 0; i < n; i++ {
		f.Segments = append(f.Segments, feeder.Segment{RatedKW: 50, ResistanceOhm: 0.02})
	}
	w := &world{f: f, reg: settlement.NewRegistry(), keys: map[string]ed25519.PrivateKey{}, n: n}
	for i := 0; i < n; i++ {
		w.enroll(fmt.Sprintf("H%d", i))
	}
	w.enroll(HeadID("Z1"))
	return w
}

func (w *world) enroll(id string) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	w.reg.Enroll(id, pub)
	w.keys[id] = priv
}

// truth lays out what physically happened and what each meter reports.
type truth struct {
	delivered map[string]float64 // seller id -> kWh actually injected above baseline
	absorbed  map[string]float64 // buyer id -> kWh actually drawn above baseline
	lie       map[string]float64 // seller id -> kWh it CLAIMS on its meter
}

func (w *world) input(baseline float64, trades []market.Trade, tr truth) Input {
	in := Input{Zone: "Z1", Start: t0, Hours: hrs, Trades: trades}
	netTruth := make([]float64, w.n)
	for i := 0; i < w.n; i++ {
		id := fmt.Sprintf("H%d", i)
		v := baseline + tr.delivered[id] - tr.absorbed[id]
		netTruth[i] = v
		reported := v
		if c, ok := tr.lie[id]; ok {
			reported = baseline + c
		}
		in.Homes = append(in.Homes, Home{ID: id, Bus: i, BaselineKWh: baseline,
			Reading: settlement.SignReading(w.keys[id], id, t0, reported)})
	}
	var sum float64
	for _, v := range netTruth {
		sum += v
	}
	head := sum - LossKWh(w.f, netTruth, hrs)
	in.Head = settlement.SignReading(w.keys[HeadID("Z1")], HeadID("Z1"), t0, head)
	return in
}

func trade(id, seller, buyer string, kwh float64) market.Trade {
	return market.Trade{TradeID: id, Zone: "Z1", IntervalStart: t0, SellerID: seller, BuyerID: buyer,
		EnergyKWh: kwh, Price: 0.15, LossFactor: 0.01, GateResult: market.GatePass}
}

func totals(r Result) (charged, paid float64) {
	for _, t := range r.Trades {
		charged += t.Charge
	}
	for _, p := range r.Payouts {
		paid += p.Amount
	}
	return
}

func twoSellersTwoBuyers() []market.Trade {
	return []market.Trade{trade("t1", "H0", "H2", 1.0), trade("t2", "H1", "H3", 1.0)}
}

func TestHonestDeliveryFullySettles(t *testing.T) {
	w := newWorld(6)
	tr := truth{delivered: map[string]float64{"H0": 1, "H1": 1}, absorbed: map[string]float64{"H2": 1, "H3": 1}}
	r := Settle(w.input(0.1, twoSellersTwoBuyers(), tr), w.reg, w.f)
	if !r.Conserved {
		t.Fatalf("honest zone should conserve: excess %.4f tol %.4f", r.ExcessKWh, r.ToleranceKWh)
	}
	for _, o := range r.Trades {
		if o.Status != StatusSettled || o.Fill < 0.99 {
			t.Fatalf("trade %s: %s fill %.3f", o.TradeID, o.Status, o.Fill)
		}
	}
	if c, p := totals(r); math.Abs(c-p) > 1e-9 {
		t.Fatalf("charged %.6f != paid %.6f", c, p)
	}
	if len(r.Transfers) != 0 {
		t.Fatalf("no penalty expected: %+v", r.Transfers)
	}
}

// A seller delivers only 30% but its meter honestly says so. Nobody can say which
// buyer was short, so the shortfall is shared by every buyer; the honest seller is
// paid in full; the breaching seller is penalised and the buyers are compensated.
func TestUnderDeliveryIsSharedAndPenalised(t *testing.T) {
	w := newWorld(6)
	tr := truth{delivered: map[string]float64{"H0": 1, "H1": 0.3}, absorbed: map[string]float64{"H2": 1, "H3": 1}}
	r := Settle(w.input(0.1, twoSellersTwoBuyers(), tr), w.reg, w.f)
	if !r.Conserved {
		t.Fatalf("an honest meter showing a shortfall still conserves: excess %.4f", r.ExcessKWh)
	}
	c, p := totals(r)
	if math.Abs(c-p) > 1e-9 {
		t.Fatalf("charged %.6f != paid %.6f", c, p)
	}
	full := 2 * 0.99 * 0.15 // both trades at full value
	if c >= full-0.01 {
		t.Fatalf("buyers should be charged less than full value %.3f, got %.3f", full, c)
	}
	// Both buyers share the shortfall, not just the buyer paired with the short seller.
	if math.Abs(r.Trades[0].Fill-r.Trades[1].Fill) > 1e-9 {
		t.Fatalf("shortfall must be socialised across buyers: %.3f vs %.3f", r.Trades[0].Fill, r.Trades[1].Fill)
	}
	paid := map[string]float64{}
	for _, po := range r.Payouts {
		paid[po.Seller] = po.Amount
	}
	if paid["H0"] <= paid["H1"] {
		t.Fatalf("honest seller must out-earn the short seller: %+v", paid)
	}
	if math.Abs(paid["H0"]-1*0.99*0.15) > 0.005 {
		t.Fatalf("honest seller should be paid ~in full, got %.4f", paid["H0"])
	}
	var pen float64
	for _, tf := range r.Transfers {
		if tf.From != "H1" || tf.Kind != "PENALTY" {
			t.Fatalf("only H1 should pay a penalty: %+v", tf)
		}
		pen += tf.Amount
	}
	if pen <= 0 {
		t.Fatal("breaching seller should be penalised")
	}
}

// The case the old per-pair check could not catch: the seller's meter claims full
// delivery that never happened. Both parties' meters agree with the trade, so a
// pair check passes. The feeder-head meter does not.
func TestLyingMeterIsCaughtByTheHeadMeter(t *testing.T) {
	w := newWorld(6)
	trades := twoSellersTwoBuyers()
	tr := truth{
		delivered: map[string]float64{"H0": 1, "H1": 0.0}, // H1 injected nothing...
		absorbed:  map[string]float64{"H2": 1, "H3": 1},
		lie:       map[string]float64{"H1": 1}, // ...but its signed meter says it delivered 1 kWh
	}
	in := w.input(0.1, trades, tr)

	// Pair check on the liar: signed reading matches the trade exactly.
	var liar Home
	for _, h := range in.Homes {
		if h.ID == "H1" {
			liar = h
		}
	}
	if d := liar.Reading.NetKWh - liar.BaselineKWh; math.Abs(d-1.0) > 1e-9 || !w.reg.Verify(liar.Reading) {
		t.Fatalf("test setup: liar should look perfect on its own meter, got %.3f", d)
	}

	r := Settle(in, w.reg, w.f)
	if r.Conserved {
		t.Fatalf("conservation should fail: excess %.4f tol %.4f", r.ExcessKWh, r.ToleranceKWh)
	}
	if r.ExcessKWh < 0.9 {
		t.Fatalf("excess should be ~1 kWh (the lie), got %.4f", r.ExcessKWh)
	}
	c, p := totals(r)
	if math.Abs(c-p) > 1e-9 {
		t.Fatalf("charged %.6f != paid %.6f", c, p)
	}
	// Only ~1 kWh was really delivered, so buyers pay for about half of 2 kWh.
	if want := 1 * 0.99 * 0.15; math.Abs(c-want) > 0.01 {
		t.Fatalf("buyers should pay only for confirmed energy ~%.4f, got %.4f", want, c)
	}
	for _, o := range r.Trades {
		if o.Status == StatusSettled {
			t.Fatalf("no trade may be fully settled when the zone does not conserve: %+v", o)
		}
	}
}

func TestForgedHeadMeterRefundsEverything(t *testing.T) {
	w := newWorld(6)
	tr := truth{delivered: map[string]float64{"H0": 1, "H1": 1}, absorbed: map[string]float64{"H2": 1, "H3": 1}}
	in := w.input(0.1, twoSellersTwoBuyers(), tr)
	in.Head.NetKWh += 0.5 // altered after signing
	r := Settle(in, w.reg, w.f)
	if r.HeadOK {
		t.Fatal("altered head reading must fail verification")
	}
	if c, _ := totals(r); c != 0 {
		t.Fatalf("nothing can be confirmed without the anchor, charged %.4f", c)
	}
	for _, o := range r.Trades {
		if o.Status != StatusDisputed {
			t.Fatalf("expected DISPUTED, got %s", o.Status)
		}
	}
}

func TestBuyerThatDoesNotDrawIsNotCharged(t *testing.T) {
	w := newWorld(6)
	// Buyer H3 never draws (battery full), so seller H1's energy goes unabsorbed.
	tr := truth{delivered: map[string]float64{"H0": 1, "H1": 1}, absorbed: map[string]float64{"H2": 1, "H3": 0}}
	r := Settle(w.input(0.1, twoSellersTwoBuyers(), tr), w.reg, w.f)
	var o3 TradeOutcome
	for _, o := range r.Trades {
		if o.TradeID == "t2" {
			o3 = o
		}
	}
	if o3.Charge > 1e-9 {
		t.Fatalf("buyer that drew nothing must not pay, charged %.4f", o3.Charge)
	}
	if c, p := totals(r); math.Abs(c-p) > 1e-9 {
		t.Fatalf("charged %.6f != paid %.6f", c, p)
	}
}

func TestTamperedHomeReadingFailsSignature(t *testing.T) {
	w := newWorld(6)
	tr := truth{delivered: map[string]float64{"H0": 1, "H1": 1}, absorbed: map[string]float64{"H2": 1, "H3": 1}}
	in := w.input(0.1, twoSellersTwoBuyers(), tr)
	in.Homes[1].Reading.NetKWh += 5 // seller edits its reading after signing
	r := Settle(in, w.reg, w.f)
	if len(r.BadMeters) != 1 || r.BadMeters[0] != "H1" {
		t.Fatalf("expected H1 flagged, got %v", r.BadMeters)
	}
}

// Whatever the physics, settlement never creates or destroys money.
func TestMoneyIsConservedUnderRandomFaults(t *testing.T) {
	w := newWorld(8)
	for k := 0; k < 300; k++ {
		d0, d1 := float64(k%5)/4, float64((k*7)%5)/4
		a2, a3 := float64((k*3)%5)/4, float64((k*11)%5)/4
		tr := truth{
			delivered: map[string]float64{"H0": d0, "H1": d1},
			absorbed:  map[string]float64{"H2": a2, "H3": a3},
		}
		if k%3 == 0 {
			tr.lie = map[string]float64{"H0": 1}
		}
		r := Settle(w.input(0.1, twoSellersTwoBuyers(), tr), w.reg, w.f)
		c, p := totals(r)
		if math.Abs(c-p) > 1e-9 {
			t.Fatalf("k=%d charged %.9f != paid %.9f", k, c, p)
		}
		for _, o := range r.Trades {
			if o.Charge < -1e-12 || o.Charge > o.Escrow+1e-12 {
				t.Fatalf("k=%d charge %.6f outside [0, escrow %.6f]", k, o.Charge, o.Escrow)
			}
		}
	}
}

func TestSettleIsDeterministic(t *testing.T) {
	w := newWorld(6)
	tr := truth{delivered: map[string]float64{"H0": 1, "H1": 0.3}, absorbed: map[string]float64{"H2": 1, "H3": 1}}
	in := w.input(0.1, twoSellersTwoBuyers(), tr)
	a, b := Settle(in, w.reg, w.f), Settle(in, w.reg, w.f)
	if fmt.Sprintf("%+v", a) != fmt.Sprintf("%+v", b) {
		t.Fatal("Settle must be a pure function of its input")
	}
}
