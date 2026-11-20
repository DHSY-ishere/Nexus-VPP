// Package delivery answers one question: did the buyers really receive the energy
// they paid for?
//
// The difficulty. Electricity is fungible. When seller A injects and buyer B draws on
// the same feeder, no measurement can say that B's electrons came from A. A check
// that asks "did seller A deliver to buyer B" has nothing physical to check.
//
// What can be measured, and what this package uses:
//
//  1. Additionality. Every home has a signed meter. A seller delivered if its export
//     rose above its committed baseline by the traded amount; a buyer received if its
//     draw rose above its baseline by the traded amount.
//  2. Conservation at an independent anchor. A utility-owned feeder-head meter signs
//     the zone's net flow. The homes' readings must add up to the head reading plus
//     cable losses. A seller whose own meter claims delivery that never happened
//     breaks this equation, and no single meter can hide it.
//  3. Pool settlement. Because delivery cannot be pinned to a pair, it is settled on
//     the zone as a pool. Money is released only for energy the pool confirms:
//     matched = min(confirmed seller delivery, confirmed buyer absorption). A seller's
//     shortfall is shared by all buyers pro rata instead of being pinned on a buyer
//     who cannot be identified. Each seller is paid for its own measured delivery, and
//     a seller caught short beyond meter tolerance pays a penalty to the buyers who
//     went unfilled.
//
// Settle is a pure function of its Input, the meter registry and the public feeder
// model. Every blockchain validator runs it independently and signs only if its
// answer matches the proposer's.
package delivery

import (
	"math"
	"sort"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/feeder"
	"github.com/DHSY-ishere/Nexus-VPP/backend/market"
	"github.com/DHSY-ishere/Nexus-VPP/backend/settlement"
)

const (
	// MeterSigmaKWh is the standard deviation of one meter's reading noise per
	// interval (a published meter-class specification).
	MeterSigmaKWh = 0.004
	// PenaltyRate is the share of an under-delivered trade's value a breaching seller pays.
	PenaltyRate = 0.25

	// Trade statuses.
	StatusSettled  = settlement.StatusSettled // fully confirmed
	StatusPartial  = "PARTIAL"                // some value refunded: shortfall or conservation failure
	StatusDisputed = settlement.StatusDisputed
)

// HeadID is the device id of a zone's feeder-head meter.
func HeadID(zone string) string { return "HEAD-" + zone }

// Home is one metered household and the baseline its reading is judged against.
type Home struct {
	ID          string             `json:"id"`
	Bus         int                `json:"bus"`
	BaselineKWh float64            `json:"baseline_kwh"` // net export expected with no trade (+ = export)
	Reading     settlement.Reading `json:"reading"`      // signed net export over the interval
}

// Input is everything needed to settle one zone for one interval.
type Input struct {
	Zone   string             `json:"zone"`
	Start  time.Time          `json:"start"`
	Hours  float64            `json:"hours"`
	Homes  []Home             `json:"homes"`
	Head   settlement.Reading `json:"head"`   // feeder-head meter: net export of the zone to the grid
	Trades []market.Trade     `json:"trades"` // as cleared, before settlement
}

// Party is one seller or buyer's measured position.
type Party struct {
	ID          string  `json:"id"`
	Role        string  `json:"role"` // SELLER or BUYER
	TradedKWh   float64 `json:"traded_kwh"`
	MeasuredKWh float64 `json:"measured_kwh"` // extra export (seller) or extra draw (buyer) over baseline
	Fill        float64 `json:"fill"`         // measured / traded, clamped to [0,1]
	Breach      bool    `json:"breach"`       // short by more than meter tolerance
	Value       float64 `json:"value"`        // dollars of trades this party is in
}

// TradeOutcome is the settlement of one trade.
type TradeOutcome struct {
	TradeID string  `json:"trade_id"`
	Escrow  float64 `json:"escrow"`
	Charge  float64 `json:"charge"`
	Fill    float64 `json:"fill"`
	Status  string  `json:"status"`
}

// Payout is what a seller receives (before the solidarity levy).
type Payout struct {
	Seller string  `json:"seller"`
	Amount float64 `json:"amount"`
}

// Transfer is a penalty moved between wallets. To == settlement.PoolID is the pool.
type Transfer struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Amount float64 `json:"amount"`
	Kind   string  `json:"kind"`
}

// Result is the deterministic outcome of Settle.
type Result struct {
	HeadOK       bool           `json:"head_ok"`
	BadMeters    []string       `json:"bad_meters"`
	SumHomesKWh  float64        `json:"sum_homes_kwh"`
	LossKWh      float64        `json:"loss_kwh"`
	HeadKWh      float64        `json:"head_kwh"`
	ExcessKWh    float64        `json:"excess_kwh"` // homes' net minus losses minus head: > 0 means reported export is not confirmed
	ToleranceKWh float64        `json:"tolerance_kwh"`
	Conserved    bool           `json:"conserved"`
	SellerValue  float64        `json:"seller_value"`  // dollars of seller delivery confirmed by their own meters
	BuyerValue   float64        `json:"buyer_value"`   // dollars of buyer absorption confirmed by their own meters
	MatchedValue float64        `json:"matched_value"` // dollars released: min of the two after the head-meter anchor
	Parties      []Party        `json:"parties"`
	Trades       []TradeOutcome `json:"trades"`
	Payouts      []Payout       `json:"payouts"`
	Transfers    []Transfer     `json:"transfers"`
	Note         string         `json:"note"`
}

// LossKWh estimates cable losses (I squared R) for the given per-bus net energies.
func LossKWh(f *feeder.Feeder, netKWh []float64, hours float64) float64 {
	inj := make([]float64, f.Buses())
	for i := range inj {
		if i < len(netKWh) && hours > 0 {
			inj[i] = netKWh[i] / hours
		}
	}
	flows := f.Flows(inj)
	var watts float64
	for k, s := range f.Segments {
		amps := flows[k] * 1000 / f.NominalV
		watts += amps * amps * s.ResistanceOhm
	}
	return watts / 1000 * hours
}

// tolerance is the shortfall a meter pair can show from noise alone.
func tradeTol(kwh float64) float64 { return math.Max(0.02, 0.15*kwh) }

type acc struct{ traded, value float64 }

func sortedKeys(m map[string]*acc) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }

// Settle verifies delivery for one zone-interval and decides who is paid what.
func Settle(in Input, reg *settlement.Registry, f *feeder.Feeder) Result {
	r := Result{BadMeters: []string{}, Parties: []Party{}, Trades: []TradeOutcome{}, Payouts: []Payout{}, Transfers: []Transfer{}}

	// 1. Signatures. A home whose reading fails verification is treated as unmetered:
	// its baseline stands in for the sum, and it earns no fill.
	idx := map[string]int{}
	bad := map[string]bool{}
	net := make([]float64, f.Buses())
	for i, h := range in.Homes {
		idx[h.ID] = i
		v := h.Reading.NetKWh
		if !reg.Verify(h.Reading) || h.Reading.DeviceID != h.ID || !h.Reading.IntervalStart.Equal(in.Start) {
			bad[h.ID] = true
			r.BadMeters = append(r.BadMeters, h.ID)
			v = h.BaselineKWh
		}
		if h.Bus >= 0 && h.Bus < len(net) {
			net[h.Bus] = v
		}
		r.SumHomesKWh += v
	}
	r.HeadOK = reg.Verify(in.Head) && in.Head.DeviceID == HeadID(in.Zone) && in.Head.IntervalStart.Equal(in.Start)
	r.HeadKWh = in.Head.NetKWh

	// 2. Conservation anchor.
	r.LossKWh = LossKWh(f, net, in.Hours)
	r.ExcessKWh = r.SumHomesKWh - r.LossKWh - r.HeadKWh
	r.ToleranceKWh = 5*MeterSigmaKWh*math.Sqrt(float64(len(in.Homes)+1)) + 0.25*r.LossKWh
	r.Conserved = r.HeadOK && math.Abs(r.ExcessKWh) <= r.ToleranceKWh

	// 3. Per-party additionality.
	sellers, buyers := map[string]*acc{}, map[string]*acc{}
	var totalEscrow, totalRecv float64
	get := func(m map[string]*acc, id string) *acc {
		a, ok := m[id]
		if !ok {
			a = &acc{}
			m[id] = a
		}
		return a
	}
	for _, t := range in.Trades {
		esc := t.Received() * t.Price
		s, b := get(sellers, t.SellerID), get(buyers, t.BuyerID)
		s.traded += t.EnergyKWh
		s.value += esc
		b.traded += t.EnergyKWh
		b.value += esc
		totalEscrow += esc
		totalRecv += t.Received()
	}
	price := 0.0
	if totalRecv > 0 {
		price = totalEscrow / totalRecv
	}
	measure := func(id string, seller bool) float64 {
		i, ok := idx[id]
		if !ok || bad[id] {
			return 0
		}
		h := in.Homes[i]
		if seller {
			return h.Reading.NetKWh - h.BaselineKWh
		}
		return h.BaselineKWh - h.Reading.NetKWh
	}
	sFill, bFill := map[string]float64{}, map[string]float64{}
	sBreach := map[string]bool{}
	bBreach := map[string]bool{}
	var sValue, bValue float64
	for _, id := range sortedKeys(sellers) {
		a := sellers[id]
		m := measure(id, true)
		fill := 0.0
		if a.traded > 0 {
			fill = clamp01(m / a.traded)
		}
		sFill[id] = fill
		sBreach[id] = a.traded-m > tradeTol(a.traded)
		sValue += fill * a.value
		r.Parties = append(r.Parties, Party{ID: id, Role: "SELLER", TradedKWh: a.traded, MeasuredKWh: m, Fill: fill, Breach: sBreach[id], Value: a.value})
	}
	for _, id := range sortedKeys(buyers) {
		a := buyers[id]
		m := measure(id, false)
		fill := 0.0
		if a.traded > 0 {
			fill = clamp01(m / a.traded)
		}
		bFill[id] = fill
		bBreach[id] = a.traded-m > tradeTol(a.traded)
		bValue += fill * a.value
		r.Parties = append(r.Parties, Party{ID: id, Role: "BUYER", TradedKWh: a.traded, MeasuredKWh: m, Fill: fill, Breach: bBreach[id], Value: a.value})
	}
	r.SellerValue, r.BuyerValue = sValue, bValue

	// 4. Pool settlement. Release only what the independent anchor can confirm.
	sConf, bConf := sValue, bValue
	switch {
	case !r.HeadOK:
		sConf, bConf = 0, 0
		r.Note = "feeder-head meter unsigned or mismatched: nothing can be confirmed, all escrow refunded"
	case r.ExcessKWh > r.ToleranceKWh:
		sConf = math.Max(0, sConf-r.ExcessKWh*price)
		r.Note = "reported export exceeds what the feeder-head meter confirms: seller side cut"
	case r.ExcessKWh < -r.ToleranceKWh:
		bConf = math.Max(0, bConf-(-r.ExcessKWh)*price)
		r.Note = "reported draw exceeds what the feeder-head meter confirms: buyer side cut"
	}
	r.MatchedValue = math.Min(sConf, bConf)

	// Sellers are paid in proportion to their own confirmed delivery.
	sShare := map[string]float64{}
	for _, id := range sortedKeys(sellers) {
		if sValue > 0 {
			sShare[id] = sFill[id] * sellers[id].value / sValue
		}
		if amt := r.MatchedValue * sShare[id]; amt > 0 {
			r.Payouts = append(r.Payouts, Payout{Seller: id, Amount: amt})
		}
	}
	// Buyers are charged in proportion to their own confirmed absorption.
	bCharge := map[string]float64{}
	unfilled := map[string]float64{}
	var unfilledTotal float64
	for _, id := range sortedKeys(buyers) {
		if bValue > 0 {
			bCharge[id] = r.MatchedValue * bFill[id] * buyers[id].value / bValue
		}
		unfilled[id] = buyers[id].value - bCharge[id]
		unfilledTotal += unfilled[id]
	}

	// 5. Per-trade outcomes.
	for _, t := range in.Trades {
		esc := t.Received() * t.Price
		b := buyers[t.BuyerID]
		charge := 0.0
		if b.value > 0 {
			charge = bCharge[t.BuyerID] * esc / b.value
		}
		fill := 1.0
		if esc > 0 {
			fill = charge / esc
		}
		short := (1 - fill) * t.EnergyKWh
		status := StatusPartial
		switch {
		// Settled means nobody breached and the zone conserved. A shortfall inside the
		// meter-noise tolerance (e.g. a trade smaller than one meter's noise) is not a breach.
		case r.Conserved && !sBreach[t.SellerID] && !bBreach[t.BuyerID] && short <= tradeTol(t.EnergyKWh):
			status = StatusSettled
		case fill < 0.02:
			status = StatusDisputed
		}
		r.Trades = append(r.Trades, TradeOutcome{TradeID: t.TradeID, Escrow: esc, Charge: charge, Fill: fill, Status: status})
	}

	// 6. Penalty: a seller short beyond tolerance pays the buyers who went unfilled.
	for _, id := range sortedKeys(sellers) {
		if !sBreach[id] {
			continue
		}
		pen := PenaltyRate * (1 - sFill[id]) * sellers[id].value
		if pen <= 0 {
			continue
		}
		if unfilledTotal <= 1e-12 {
			r.Transfers = append(r.Transfers, Transfer{From: id, To: settlement.PoolID, Amount: pen, Kind: "PENALTY"})
			continue
		}
		for _, bid := range sortedKeys(buyers) {
			if share := pen * unfilled[bid] / unfilledTotal; share > 1e-12 {
				r.Transfers = append(r.Transfers, Transfer{From: id, To: bid, Amount: share, Kind: "PENALTY"})
			}
		}
	}
	return r
}
