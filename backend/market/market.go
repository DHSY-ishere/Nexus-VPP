// Package market is L4: a uniform-price call auction per feeder zone followed by a
// network gate.
//
// What is traded. Passive net positions (solar minus load) already flow through
// the feeder whatever the market does. The market therefore clears *flexibility
// increments* per delivery interval: a seller injects extra energy (battery
// discharge, avoided curtailment), a buyer absorbs extra energy (battery or EV
// charging, flexible load). Those increments are what can overload a cable or
// push voltage out of range, so those are what the gate checks.
package market

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/feeder"
)

type Side int

const (
	Sell Side = iota
	Buy
)

// Network is what the gate needs to know about a feeder: how stressed a set of bus
// injections is, and the cable resistance between two buses (for loss estimates).
// *feeder.Feeder implements it from a known model; twin.LearnedNetwork implements
// it from probe data with no impedance model.
type Network interface {
	Stress(inj []float64) float64
	PathResistance(a, b int) float64
}

// Gate results recorded on each trade.
const (
	GatePass      = "PASS"      // matched with the first-choice counterparty, no limit hit
	GateRerouted  = "REROUTED"  // a nearer counterparty was blocked, matched with another
	GateCurtailed = "CURTAILED" // volume cut so the feeder stays inside its limits
)

// Step is one tranche of a bid curve: up to KWh at the given price per kWh
// (a floor for sellers, a ceiling for buyers).
type Step struct {
	KWh   float64 `json:"kwh"`
	Price float64 `json:"price"`
}

// Bid is one agent's bid curve for one zone and interval.
type Bid struct {
	AgentID  string
	Zone     string
	Bus      int
	Side     Side
	Priority int // island mode only: 0 normal, 1 essential, 2 life-critical
	Steps    []Step
}

// Options for one clearing run.
type Options struct {
	Interval time.Duration
	Island   bool    // islanded microgrid: serve by priority before price
	PriceCap float64 // upper bound on the clearing price; 0 = none
}

// Trade is one matched pair after the network gate.
type Trade struct {
	TradeID       string    `json:"trade_id"`
	Zone          string    `json:"zone"`
	IntervalStart time.Time `json:"interval_start"`
	SellerID      string    `json:"seller_id"`
	BuyerID       string    `json:"buyer_id"`
	SellerBus     int       `json:"seller_bus"`
	BuyerBus      int       `json:"buyer_bus"`
	EnergyKWh     float64   `json:"energy_kwh"` // injected by the seller
	Price         float64   `json:"price"`      // per kWh, uniform within the zone
	LossFactor    float64   `json:"loss_factor"`
	GateResult    string    `json:"gate_result"`
	Priority      int       `json:"priority"`

	// Filled in by settlement.
	MeasuredKWh     float64 `json:"measured_kwh"`
	StabilityCredit float64 `json:"stability_credit"`
	Levy            float64 `json:"levy"`
	Status          string  `json:"status"`
	LeafHash        string  `json:"leaf_hash"`
}

// Received is the energy the buyer actually gets after network losses.
func (t Trade) Received() float64 { return t.EnergyKWh * (1 - t.LossFactor) }

// Result is the outcome for one zone and interval.
type Result struct {
	Zone         string
	Price        float64
	AuctionKWh   float64 // what the auction alone would have cleared
	ClearedKWh   float64 // what the network gate allowed
	GatedKWh     float64
	Trades       []Trade
	StressBefore float64
	StressAfter  float64
	Injections   []float64 // final bus injections in kW, baseline plus trades
}

type unit struct {
	idx   int
	kwh   float64
	price float64
	prio  int
}

// ClearZone runs the auction for one zone, then pairs buyers with sellers by
// electrical distance, cutting any pairing that would worsen the feeder.
// baseline is the passive net injection per bus in kW.
func ClearZone(f *feeder.Feeder, baseline []float64, bids []Bid, start time.Time, opt Options) Result {
	return ClearZoneWith(f.Zone, f.NominalV, f, baseline, bids, start, opt)
}

// ClearZoneWith is ClearZone with an explicit Network, so the gate can run on a
// learned network instead of the true feeder model.
func ClearZoneWith(zone string, nominalV float64, f Network, baseline []float64, bids []Bid, start time.Time, opt Options) Result {
	res := Result{Zone: zone}
	h := opt.Interval.Hours()
	inj := append([]float64(nil), baseline...)
	res.StressBefore = f.Stress(inj)

	var sells, buys []unit
	for i, b := range bids {
		for _, s := range b.Steps {
			if s.KWh <= 0 {
				continue
			}
			u := unit{i, s.KWh, s.Price, b.Priority}
			if b.Side == Sell {
				sells = append(sells, u)
			} else {
				buys = append(buys, u)
			}
		}
	}
	sort.SliceStable(sells, func(a, b int) bool { return sells[a].price < sells[b].price })
	sort.SliceStable(buys, func(a, b int) bool {
		if opt.Island && buys[a].prio != buys[b].prio {
			return buys[a].prio > buys[b].prio
		}
		return buys[a].price > buys[b].price
	})

	sellAlloc := map[int]float64{}
	buyAlloc := map[int]float64{}
	var lastS, lastB, total float64
	si, bi := 0, 0
	for si < len(sells) && bi < len(buys) {
		s, b := &sells[si], &buys[bi]
		if !(s.price <= b.price || (opt.Island && b.prio > 0)) {
			break
		}
		q := math.Min(s.kwh, b.kwh)
		sellAlloc[s.idx] += q
		buyAlloc[b.idx] += q
		s.kwh -= q
		b.kwh -= q
		total += q
		lastS, lastB = s.price, b.price
		if s.kwh <= 1e-9 {
			si++
		}
		if b.kwh <= 1e-9 {
			bi++
		}
	}
	res.AuctionKWh = total
	if total <= 0 {
		res.Injections = inj
		res.StressAfter = res.StressBefore
		return res
	}
	res.Price = (lastS + lastB) / 2
	if opt.PriceCap > 0 && res.Price > opt.PriceCap {
		res.Price = opt.PriceCap
	}

	// Buyers in service order. Every seller whose ask is at or below the marginal ask
	// is in the money and is paid the uniform price whichever buyer it serves, so
	// the volume is allocated among them by electrical distance, not list order.
	var buyers []int
	for i := range bids {
		if buyAlloc[i] > 1e-9 {
			buyers = append(buyers, i)
		}
	}
	sort.SliceStable(buyers, func(a, b int) bool { return bids[buyers[a]].Priority > bids[buyers[b]].Priority })
	var sellers []int
	sellAlloc = map[int]float64{}
	for i, b := range bids {
		if b.Side != Sell {
			continue
		}
		for _, s := range b.Steps {
			if s.KWh > 0 && s.Price <= lastS {
				sellAlloc[i] += s.KWh
			}
		}
		if sellAlloc[i] > 1e-9 {
			sellers = append(sellers, i)
		}
	}

	n := 0
	for _, bIdx := range buyers {
		buyer := bids[bIdx]
		need := buyAlloc[bIdx]
		order := append([]int(nil), sellers...)
		sort.SliceStable(order, func(a, b int) bool {
			return abs(bids[order[a]].Bus-buyer.Bus) < abs(bids[order[b]].Bus-buyer.Bus)
		})
		blocked := false
		for _, sIdx := range order {
			if need <= 1e-9 {
				break
			}
			rem := sellAlloc[sIdx]
			if rem <= 1e-9 {
				continue
			}
			seller := bids[sIdx]
			want := math.Min(need, rem)
			q := maxFeasible(f, inj, seller.Bus, buyer.Bus, want, h)
			cut := q < want-1e-6
			if q > 1e-6 {
				inj[seller.Bus] += q / h
				inj[buyer.Bus] -= q / h
				gate := GatePass
				switch {
				case cut:
					gate = GateCurtailed
				case blocked:
					gate = GateRerouted
				}
				pKW := q / h
				loss := f.PathResistance(seller.Bus, buyer.Bus) * pKW * 1000 / (nominalV * nominalV)
				n++
				res.Trades = append(res.Trades, Trade{
					TradeID:       fmt.Sprintf("%s-%s-%03d", zone, start.Format("0102T1504"), n),
					Zone:          zone,
					IntervalStart: start,
					SellerID:      seller.AgentID,
					BuyerID:       buyer.AgentID,
					SellerBus:     seller.Bus,
					BuyerBus:      buyer.Bus,
					EnergyKWh:     q,
					Price:         res.Price,
					LossFactor:    math.Min(loss, 0.2),
					GateResult:    gate,
					Priority:      buyer.Priority,
					Status:        "CLEARED",
				})
				res.ClearedKWh += q
				sellAlloc[sIdx] -= q
				need -= q
			}
			if cut {
				blocked = true
			}
		}
	}
	res.GatedKWh = math.Max(0, res.AuctionKWh-res.ClearedKWh)
	res.StressAfter = f.Stress(inj)
	res.Injections = inj
	return res
}

// maxFeasible returns the largest q in [0, want] such that adding q kWh of
// injection at sBus and the same absorption at bBus does not raise feeder stress.
// Stress is convex, so the feasible set is an interval containing 0 and bisection
// finds its edge.
func maxFeasible(f Network, inj []float64, sBus, bBus int, want, h float64) float64 {
	base := f.Stress(inj)
	trial := make([]float64, len(inj))
	ok := func(q float64) bool {
		copy(trial, inj)
		trial[sBus] += q / h
		trial[bBus] -= q / h
		return f.Stress(trial) <= base+1e-9
	}
	if ok(want) {
		return want
	}
	lo, hi := 0.0, want
	for i := 0; i < 30; i++ {
		mid := (lo + hi) / 2
		if ok(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
