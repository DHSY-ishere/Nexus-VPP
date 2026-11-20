// Package engine wires the layers together: edge agents (L2) bid, the market (L4)
// clears and gates, meters report and escrow settles (L5), and the twin (L6) scores
// each contributor. One Step is one 5-minute market interval of simulated time.
package engine

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/agent"
	"github.com/DHSY-ishere/Nexus-VPP/backend/chain"
	"github.com/DHSY-ishere/Nexus-VPP/backend/delivery"
	"github.com/DHSY-ishere/Nexus-VPP/backend/feeder"
	"github.com/DHSY-ishere/Nexus-VPP/backend/market"
	"github.com/DHSY-ishere/Nexus-VPP/backend/settlement"
	"github.com/DHSY-ishere/Nexus-VPP/backend/twin"
)

// Market constants ($/kWh).
const (
	RetailPrice   = 0.30
	FeedInPrice   = 0.06
	LevyRate      = 0.02 // solidarity pool share of every settled trade
	StabilityRate = 0.50 // $ paid per unit of stress removed, funded by the network operator
	ReflexPrice   = 0.50
	IslandCap     = 1.00
)

// Gate modes.
const (
	GateLearned = "learned" // impedance learned by home probes; thermal-only during warm-up
	GateModel   = "model"   // true feeder model (what a utility with a perfect network model could do)
)

const (
	probesPerEvent = 8    // voltage readings taken while one home steps its battery power
	homesPerProbe  = 2    // homes probing per zone per interval; staggered so probes never overlap
	minProbes      = 6    // observations a bus needs before its sensitivity is trusted
	meterNoiseV    = 0.03 // 30 mV voltage measurement noise
	voltageMargin  = 0.003
)

// Config for a simulated community.
type Config struct {
	Zones        int
	BusesPerZone int
	Tick         time.Duration // wall-clock time between intervals in Run
	Interval     time.Duration // simulated market interval
	StartHour    float64
	Seed         int64
	Gate         string // "learned" (default) or "model": which network view the market gate uses
	Chain        bool   // settle through the validator blockchain
	Validators   int    // validators in the chain (quorum is 2f+1 for n=3f+1)
	RetainBlocks int    // blocks kept in memory (0 = all)
}

// DefaultConfig is four feeders of ten homes, one 5-minute interval every 3 seconds.
func DefaultConfig() Config {
	return Config{Zones: 4, BusesPerZone: 10, Tick: 3 * time.Second, Interval: 5 * time.Minute, StartHour: 9, Seed: 42, Gate: GateLearned, Chain: true, Validators: 4, RetainBlocks: 2000}
}

type home struct {
	*agent.Agent
	priv    ed25519.PrivateKey
	savings float64 // cumulative community-market benefit vs retail / feed-in
}

// ZoneSummary is one feeder's view of an interval.
type ZoneSummary struct {
	Zone         string  `json:"zone"`
	Price        float64 `json:"price"`
	AuctionKWh   float64 `json:"auction_kwh"`
	ClearedKWh   float64 `json:"cleared_kwh"`
	GatedKWh     float64 `json:"gated_kwh"`
	Trades       int     `json:"trades"`
	StressBefore float64 `json:"stress_before"`
	StressAfter  float64 `json:"stress_after"`
	PeakLoadPct  float64 `json:"peak_load_pct"` // worst segment loading after trades
	MinPU        float64 `json:"min_pu"`
	MaxPU        float64 `json:"max_pu"`

	// Learned-gate reporting. StressBefore/After above are measured on the true
	// physics, whichever network view the gate used.
	GateMode        string  `json:"gate_mode"`     // LEARNED, WARMING_UP or MODEL
	BusesLearned    int     `json:"buses_learned"` // buses with enough probes
	Buses           int     `json:"buses"`
	SensErrPct      float64 `json:"sens_err_pct"`      // mean error of learned vs true sensitivity
	ModelClearedKWh float64 `json:"model_cleared_kwh"` // what a perfect-model gate would have cleared

	// Pool delivery check (see package delivery).
	Conserved    bool    `json:"conserved"`     // homes' meters add up to the independent feeder-head meter
	ExcessKWh    float64 `json:"excess_kwh"`    // homes' net - losses - head meter; ~0 when honest
	ToleranceKWh float64 `json:"tolerance_kwh"` // meter-noise allowance
	LossKWh      float64 `json:"loss_kwh"`
	ClaimedUSD   float64 `json:"claimed_usd"`   // value of the cleared trades
	ConfirmedUSD float64 `json:"confirmed_usd"` // value the pool delivery check released
}

// IntervalRecord is published after every interval.
type IntervalRecord struct {
	Type             string         `json:"type"`
	Interval         time.Time      `json:"interval"`
	Mode             string         `json:"mode"` // GRID_CONNECTED or ISLANDED
	Cloud            float64        `json:"cloud"`
	Zones            []ZoneSummary  `json:"zones"`
	Trades           []market.Trade `json:"trades"`
	MerkleRoot       string         `json:"merkle_root"`
	Disputed         int            `json:"disputed"`
	StressRemoved    float64        `json:"stress_removed"` // counterfactual improvement
	ReflexKW         float64        `json:"reflex_kw"`
	PoolBalance      float64        `json:"pool_balance"`
	CommunitySavings float64        `json:"community_savings"`
	Gini             float64        `json:"gini"`

	// Demand response (OpenADR).
	DRLevel int     `json:"dr_level"`
	DRPrice float64 `json:"dr_price"`

	// Blockchain.
	ChainOn     bool   `json:"chain_on"`
	BlockHeight uint64 `json:"block_height"`
	BlockHash   string `json:"block_hash"`
	BlockSigs   int    `json:"block_sigs"`
	Quorum      int    `json:"quorum"`
	Validators  int    `json:"validators"`
	Proposer    string `json:"proposer"`
	ChainErr    string `json:"chain_err"`
}

type stored struct {
	root     string
	leaves   []string
	tradeIDs []string
}

// Proof lets a household verify its trade is in the published interval root.
type Proof struct {
	TradeID string                 `json:"trade_id"`
	Leaf    string                 `json:"leaf"`
	Root    string                 `json:"root"`
	Steps   []settlement.ProofStep `json:"steps"`
	Valid   bool                   `json:"valid"`
	Trade   market.Trade           `json:"trade"`
}

// Engine is the running market simulation. All methods are safe for concurrent use.
type Engine struct {
	cfg     Config
	rng     *rand.Rand
	feeders []*feeder.Feeder
	homes   [][]*home
	reg     *settlement.Registry
	ledger  *settlement.Ledger
	publish func(any)
	persist func(IntervalRecord)

	learners [][]*twin.SensitivityLearner // per zone, per bus
	probeAt  []int                        // round-robin probe cursor per zone

	mu         sync.Mutex
	simTime    time.Time
	island     bool
	cloud      float64
	freqHz     float64 // 0 = nominal
	tamper     bool    // a seller delivers 30% of what it sold, honestly metered
	lie        bool    // a seller delivers 30% and its meter claims 100%
	drLevel    int     // demand-response level from an OpenADR event (0 = none)
	drPrice    float64 // OpenADR price signal, $/kWh (0 = none)
	headKeys   []ed25519.PrivateKey
	chain      *chain.Chain
	last       IntervalRecord
	hasLast    bool
	roots      []stored
	tradeLog   map[string]market.Trade
	tradeRoot  map[string]int
	totalGated float64
}

// New builds a community. publish and persist may be nil.
func New(cfg Config, publish func(any), persist func(IntervalRecord)) *Engine {
	e := &Engine{
		cfg: cfg, rng: rand.New(rand.NewSource(cfg.Seed)),
		reg: settlement.NewRegistry(), ledger: settlement.NewLedger(),
		publish: publish, persist: persist,
		cloud: 1, tradeLog: map[string]market.Trade{}, tradeRoot: map[string]int{},
	}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	e.simTime = day.Add(time.Duration(cfg.StartHour * float64(time.Hour)))

	for z := 0; z < cfg.Zones; z++ {
		f := &feeder.Feeder{Zone: fmt.Sprintf("Z%d", z+1), NominalV: 230, MinPU: 0.94, MaxPU: 1.06}
		n := cfg.BusesPerZone
		var hs []*home
		for b := 0; b < n; b++ {
			// Cable gets thinner (higher R, lower rating) toward the far end.
			f.Segments = append(f.Segments, feeder.Segment{
				RatedKW:       math.Max(12, 4.5*float64(n-b)+6),
				ResistanceOhm: 0.016 + 0.0027*float64(b),
			})
			a := &agent.Agent{
				ID: fmt.Sprintf("Z%d-H%02d", z+1, b+1), Zone: f.Zone, Bus: b,
				CapKWh: 10, MaxKW: 5, SoC: 30 + e.rng.Float64()*50,
				BaseLoadKW: 0.5 + e.rng.Float64()*0.7,
				Limits: agent.Limits{
					ReserveSoC: 25, MinSellPrice: 0.08 + e.rng.Float64()*0.04,
					MaxBuyPrice: 0.20 + e.rng.Float64()*0.08,
					Critical:    e.rng.Float64() < 0.15,
				},
				WearCostPerKWh: 0.03,
			}
			if e.rng.Float64() < 0.75 { // some homes (renters, shaded roofs) have no solar
				a.SolarKWp = 3 + e.rng.Float64()*3
			}
			if e.rng.Float64() < 0.5 {
				a.Reflex = &agent.Reflex{HzBelow: 49.8, KWPerHz: 8, VoltBelow: 218, KWPerVolt: 0.3, PricePerK: ReflexPrice}
			}
			pub, priv, _ := ed25519.GenerateKey(nil)
			e.reg.Enroll(a.ID, pub)
			hs = append(hs, &home{Agent: a, priv: priv})
		}
		e.feeders = append(e.feeders, f)
		e.homes = append(e.homes, hs)
		ls := make([]*twin.SensitivityLearner, n)
		for i := range ls {
			ls[i] = &twin.SensitivityLearner{}
		}
		e.learners = append(e.learners, ls)
		e.probeAt = append(e.probeAt, 0)

		// The independent feeder-head meter (utility-owned) that anchors conservation.
		hpub, hpriv, _ := ed25519.GenerateKey(nil)
		e.reg.Enroll(delivery.HeadID(f.Zone), hpub)
		e.headKeys = append(e.headKeys, hpriv)
	}
	if cfg.Chain {
		feeders := map[string]*feeder.Feeder{}
		for _, f := range e.feeders {
			feeders[f.Zone] = f
		}
		e.chain = chain.New(cfg.Validators, chain.Oracle{Reg: e.reg, Feeders: feeders}, cfg.RetainBlocks)
	}
	return e
}

// Chain returns the settlement blockchain (nil if disabled).
func (e *Engine) Chain() *chain.Chain { return e.chain }

// ---- controls ----

func (e *Engine) SetIsland(v bool)        { e.mu.Lock(); e.island = v; e.mu.Unlock() }
func (e *Engine) SetCloud(f float64)      { e.mu.Lock(); e.cloud = f; e.mu.Unlock() }
func (e *Engine) SetFrequency(hz float64) { e.mu.Lock(); e.freqHz = hz; e.mu.Unlock() }
func (e *Engine) SetTamper(v bool)        { e.mu.Lock(); e.tamper = v; e.mu.Unlock() }
func (e *Engine) SetMeterLie(v bool)      { e.mu.Lock(); e.lie = v; e.mu.Unlock() }

// SetDR applies a demand-response signal (for example from an OpenADR event): level 0-3
// lowers every home's target state of charge, so homes buy less and offer more, and a
// non-zero price caps what homes will bid to buy.
func (e *Engine) SetDR(level int, price float64) {
	e.mu.Lock()
	e.drLevel, e.drPrice = level, price
	e.mu.Unlock()
}

// ClearFaults restores nominal conditions.
func (e *Engine) ClearFaults() {
	e.mu.Lock()
	e.island, e.cloud, e.freqHz, e.tamper, e.lie, e.drLevel, e.drPrice = false, 1, 0, false, false, 0, 0
	e.mu.Unlock()
}

// ---- profiles ----

func gauss(x, mu, s float64) float64 { return math.Exp(-(x - mu) * (x - mu) / (2 * s * s)) }

func solarKW(h *home, hour, cloud float64) float64 {
	if hour < 6 || hour > 18 {
		return 0
	}
	return h.SolarKWp * math.Sin(math.Pi*(hour-6)/12) * cloud
}

func loadKW(h *home, hour float64) float64 {
	return h.BaseLoadKW * (1 + 1.8*gauss(hour, 19, 2) + 0.6*gauss(hour, 8, 1.2))
}

// Step simulates one market interval and returns its record.
func (e *Engine) Step() IntervalRecord {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := e.simTime
	hoursLong := e.cfg.Interval.Hours()
	hour := float64(start.Hour()) + float64(start.Minute())/60
	opt := market.Options{Interval: e.cfg.Interval, Island: e.island}
	if e.island {
		opt.PriceCap = IslandCap
	}
	mode := "GRID_CONNECTED"
	if e.island {
		mode = "ISLANDED"
	}

	rec := IntervalRecord{Type: "MARKET_INTERVAL", Interval: start, Mode: mode, Cloud: e.cloud, DRLevel: e.drLevel, DRPrice: e.drPrice}
	var allTrades []market.Trade
	var zrs []chain.ZoneRecord
	var credits []credit
	var improvement float64
	var contribs []twin.Contribution
	var bases [][]float64

	// Reflex responses: local, instant, settled afterwards from the meter.
	if e.freqHz > 0 {
		for _, hs := range e.homes {
			for _, h := range hs {
				if kw := h.Respond(e.freqHz, 0); kw > 0 {
					kwh := kw * hoursLong
					h.SoC -= kwh / h.CapKWh * 100
					e.ledger.Credit(h.ID, kwh*ReflexPrice)
					rec.ReflexKW += kw
				}
			}
		}
	}

	for z, f := range e.feeders {
		hs := e.homes[z]
		baseline := make([]float64, len(hs))
		for i, h := range hs {
			baseline[i] = solarKW(h, hour, e.cloud) - loadKW(h, hour)
		}
		var bids []market.Bid
		for _, h := range hs {
			h.DRLevel, h.DRCeiling = e.drLevel, e.drPrice
			if b, ok := h.Bid(hour, hoursLong, e.island); ok {
				bids = append(bids, b)
			}
		}
		e.probe(z)
		net, gateMode, learned := e.network(z)
		res := market.ClearZoneWith(f.Zone, f.NominalV, net, baseline, bids, start, opt)
		bases = append(bases, baseline)
		// Shadow run with the true model, to measure what the learned gate costs.
		modelRes := market.ClearZone(f, baseline, bids, start, opt)
		stressBefore, stressAfter := f.Stress(baseline), f.Stress(res.Injections)

		// Faults: the first seller this interval under-delivers (honest meter), or
		// under-delivers while its meter claims full delivery (lying meter).
		tamperID, lieID := "", ""
		if len(res.Trades) > 0 {
			if e.tamper {
				tamperID = res.Trades[0].SellerID
			}
			if e.lie {
				lieID = res.Trades[0].SellerID
			}
		}
		zr := e.buildZone(z, f, hs, baseline, res.Trades, start, hoursLong, tamperID, lieID)
		zrs = append(zrs, zr)
		zoneTrades := chain.FinalTrades(zr)
		measured := map[string]float64{}
		traded := map[string]float64{}
		for _, p := range zr.Result.Parties {
			if p.Role == "SELLER" {
				measured[p.ID], traded[p.ID] = p.MeasuredKWh, p.TradedKWh
			}
		}
		for i := range zoneTrades {
			if tt := traded[zoneTrades[i].SellerID]; tt > 0 {
				zoneTrades[i].MeasuredKWh = measured[zoneTrades[i].SellerID] * zoneTrades[i].EnergyKWh / tt
			}
		}
		allTrades = append(allTrades, zoneTrades...)

		// Counterfactual attribution over verified trades only.
		for _, tr := range allTrades {
			if tr.Zone != f.Zone || tr.Status != settlement.StatusSettled {
				continue
			}
			p := tr.EnergyKWh / hoursLong
			contribs = append(contribs, twin.Contribution{ID: tr.TradeID, Effects: []twin.Effect{
				{Bus: tr.SellerBus, KW: p}, {Bus: tr.BuyerBus, KW: -p}}})
		}
		shares, total := twin.ShapleyAttribution(net, baseline, contribs, 24, e.rng)
		improvement += total
		// Each trade's credit is split evenly between its seller and its buyer.
		for i := range allTrades {
			tr := &allTrades[i]
			if tr.Zone != f.Zone {
				continue
			}
			if share := shares[tr.TradeID]; share > 0 {
				c := share * StabilityRate
				credits = append(credits, credit{tr.SellerID, c / 2}, credit{tr.BuyerID, c / 2})
				tr.StabilityCredit = c
			}
		}
		contribs = contribs[:0]

		flows := f.Flows(res.Injections)
		peak := 0.0
		for k, s := range f.Segments {
			peak = math.Max(peak, math.Abs(flows[k])/s.RatedKW*100)
		}
		minPU, maxPU := 9.0, 0.0
		for _, v := range f.Voltages(res.Injections) {
			minPU, maxPU = math.Min(minPU, v/f.NominalV), math.Max(maxPU, v/f.NominalV)
		}
		rec.Zones = append(rec.Zones, ZoneSummary{
			Zone: f.Zone, Price: res.Price, AuctionKWh: res.AuctionKWh, ClearedKWh: res.ClearedKWh,
			GatedKWh: res.GatedKWh, Trades: len(res.Trades),
			StressBefore: stressBefore, StressAfter: stressAfter,
			PeakLoadPct: peak, MinPU: minPU, MaxPU: maxPU,
			GateMode: gateMode, BusesLearned: learned, Buses: f.Buses(),
			SensErrPct: e.sensitivityError(z), ModelClearedKWh: modelRes.ClearedKWh,
			Conserved: zr.Result.Conserved, ExcessKWh: zr.Result.ExcessKWh,
			ToleranceKWh: zr.Result.ToleranceKWh, LossKWh: zr.Result.LossKWh,
			ClaimedUSD: claimedUSD(zr), ConfirmedUSD: zr.Result.MatchedValue,
		})
		e.totalGated += res.GatedKWh
	}

	// Commit the interval to the blockchain. Money moves only if a quorum of validators
	// independently reproduced the settlement.
	committed := true
	if e.chain != nil {
		blk, err := e.chain.Propose(start, zrs)
		rec.ChainOn, rec.Quorum, rec.Validators = true, e.chain.Quorum(), len(e.chain.Info().Validators)
		if err != nil {
			committed = false
			rec.ChainErr = err.Error()
			for i := range allTrades { // nothing settles without consensus
				allTrades[i].Status, allTrades[i].Fill, allTrades[i].Charged = delivery.StatusDisputed, 0, 0
			}
		} else {
			rec.BlockHeight, rec.BlockHash, rec.BlockSigs, rec.Proposer = blk.Height, blk.Hash, len(blk.Sigs), blk.Proposer
		}
	}
	if committed {
		byID := map[string]int{}
		for i := range allTrades {
			byID[allTrades[i].TradeID] = i
		}
		for z, zr := range zrs {
			e.applyMoney(zr, e.homes[z], allTrades, byID)
		}
		for _, c := range credits {
			e.ledger.Credit(c.id, c.amt)
		}
	}

	// Passive battery dynamics: surplus charges, deficit discharges (70% efficient to the net).
	for z, hs := range e.homes {
		for i, h := range hs {
			h.SoC = math.Max(0, math.Min(100, h.SoC+bases[z][i]*hoursLong/h.CapKWh*100*0.7))
		}
	}

	// Merkle root over every trade in the interval, in a stable order.
	sort.SliceStable(allTrades, func(a, b int) bool { return allTrades[a].TradeID < allTrades[b].TradeID })
	leaves := make([]string, len(allTrades))
	ids := make([]string, len(allTrades))
	for i := range allTrades {
		allTrades[i].LeafHash = settlement.LeafHash(chain.Leaf(allTrades[i]))
		leaves[i], ids[i] = allTrades[i].LeafHash, allTrades[i].TradeID
	}
	root := settlement.MerkleRoot(leaves)
	e.roots = append(e.roots, stored{root, leaves, ids})
	for _, tr := range allTrades {
		e.tradeLog[tr.TradeID] = tr
		e.tradeRoot[tr.TradeID] = len(e.roots) - 1
		if tr.Status != settlement.StatusSettled { // partial or disputed: something was refunded
			rec.Disputed++
		}
	}
	if len(e.roots) > 120 { // keep two hours of proofs
		drop := len(e.roots) - 120
		for id, r := range e.tradeRoot {
			if r < drop {
				delete(e.tradeRoot, id)
				delete(e.tradeLog, id)
			} else {
				e.tradeRoot[id] = r - drop
			}
		}
		e.roots = e.roots[drop:]
	}

	rec.Trades = allTrades
	rec.MerkleRoot = root
	rec.StressRemoved = improvement
	rec.PoolBalance = e.ledger.Pool
	rec.CommunitySavings, rec.Gini = e.fairness()
	e.last, e.hasLast = rec, true
	e.simTime = start.Add(e.cfg.Interval)
	return rec
}

// probe has a few homes in zone z step their battery power slightly and record
// how their own voltage responds. Homes take turns so that two probes never
// overlap in time; otherwise one home's voltage change would be blamed on another.
func (e *Engine) probe(z int) {
	f := e.feeders[z]
	n := f.Buses()
	for k := 0; k < homesPerProbe; k++ {
		bus := e.probeAt[z] % n
		e.probeAt[z]++
		for p := 0; p < probesPerEvent; p++ {
			dP := 0.3 + 0.5*e.rng.Float64()
			if e.rng.Intn(2) == 0 {
				dP = -dP
			}
			dV := f.Sensitivity(bus, bus)*dP + e.rng.NormFloat64()*meterNoiseV
			e.learners[z][bus].Observe(dP, dV)
		}
	}
}

// network returns the view the market gate uses for zone z, a label for it, and
// how many buses have enough probe data.
func (e *Engine) network(z int) (market.Network, string, int) {
	f := e.feeders[z]
	ready := 0
	for _, l := range e.learners[z] {
		if l.N >= minProbes {
			ready++
		}
	}
	if e.cfg.Gate == GateModel {
		return f, "MODEL", ready
	}
	n := twin.LearnedNetwork{
		NominalV: f.NominalV, MinPU: f.MinPU, MaxPU: f.MaxPU, MarginPU: voltageMargin,
		Self: make([]float64, f.Buses()), RatedKW: make([]float64, f.Buses()),
		UseVoltage: ready == f.Buses(),
	}
	for i, seg := range f.Segments {
		n.RatedKW[i] = seg.RatedKW
		n.Self[i] = e.learners[z][i].Estimate()
	}
	mode := "WARMING_UP"
	if n.UseVoltage {
		mode = "LEARNED"
	}
	return n, mode, ready
}

// sensitivityError is the mean relative error (percent) of the learned
// self-sensitivities against the true ones, over buses that have data.
func (e *Engine) sensitivityError(z int) float64 {
	f := e.feeders[z]
	sum, n := 0.0, 0
	for i, l := range e.learners[z] {
		if l.N == 0 {
			continue
		}
		truth := f.Sensitivity(i, i)
		sum += math.Abs(l.Estimate()-truth) / truth
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n) * 100
}

// settle dispatches one trade: escrow, signed meter readings, delivery check, payout.
type credit struct {
	id  string
	amt float64
}

func claimedUSD(zr chain.ZoneRecord) float64 {
	var v float64
	for _, t := range zr.Result.Trades {
		v += t.Escrow
	}
	return v
}

// buildZone produces the evidence for one zone-interval and its settlement outcome.
//
// It first decides what physically happened ("truth"): every home's net energy, with a
// faulty seller delivering only 30% of what it sold. Each home's meter then signs a
// reading with noise. An honest meter reports the truth; a lying meter reports the
// full amount its seller sold. The feeder-head meter is independent of every home: it
// measures the real zone total minus real cable losses.
func (e *Engine) buildZone(z int, f *feeder.Feeder, hs []*home, baseline []float64, trades []market.Trade,
	start time.Time, hours float64, tamperID, lieID string) chain.ZoneRecord {

	truth := make([]float64, len(hs))
	claimed := make([]float64, len(hs))
	for i := range hs {
		truth[i] = baseline[i] * hours
	}
	for _, t := range trades {
		d := t.EnergyKWh
		if t.SellerID == tamperID || t.SellerID == lieID {
			d *= 0.3
		}
		truth[t.SellerBus] += d
		truth[t.BuyerBus] -= t.EnergyKWh
	}
	copy(claimed, truth)
	for _, t := range trades {
		if t.SellerID == lieID {
			claimed[t.SellerBus] += 0.7 * t.EnergyKWh // meter claims the full sale
		}
	}

	noise := func() float64 { return e.rng.NormFloat64() * delivery.MeterSigmaKWh }
	in := delivery.Input{Zone: f.Zone, Start: start, Hours: hours, Trades: trades}
	var sum float64
	for i, h := range hs {
		sum += truth[i]
		in.Homes = append(in.Homes, delivery.Home{
			ID: h.ID, Bus: i, BaselineKWh: baseline[i] * hours,
			Reading: settlement.SignReading(h.priv, h.ID, start, claimed[i]+noise()),
		})
	}
	head := sum - delivery.LossKWh(f, truth, hours) + noise()
	in.Head = settlement.SignReading(e.headKeys[z], delivery.HeadID(f.Zone), start, head)
	return chain.ZoneRecord{Input: in, Result: delivery.Settle(in, e.reg, f)}
}

// applyMoney moves the money a committed settlement decided: escrow is held, closed
// for the confirmed charge (the rest returns to the buyer), the charge is paid out to
// sellers pro rata to confirmed delivery, and penalties are transferred.
func (e *Engine) applyMoney(zr chain.ZoneRecord, hs []*home, all []market.Trade, byID map[string]int) {
	homes := map[string]*home{}
	for _, h := range hs {
		homes[h.ID] = h
	}
	res := zr.Result
	for i, t := range zr.Input.Trades {
		e.ledger.Hold(t.TradeID, t.BuyerID, res.Trades[i].Escrow)
	}
	for _, o := range res.Trades {
		e.ledger.Close(o.TradeID, o.Charge)
	}
	levy := map[string]float64{}
	for _, p := range res.Payouts {
		levy[p.Seller] = e.ledger.Pay(p.Seller, p.Amount, LevyRate)
	}
	for _, tf := range res.Transfers {
		e.ledger.Transfer(tf.From, tf.To, tf.Amount)
	}
	sellerValue := map[string]float64{}
	for _, p := range res.Parties {
		if p.Role == "SELLER" {
			sellerValue[p.ID] = p.Value
		}
	}
	for i, t := range zr.Input.Trades {
		o := res.Trades[i]
		idx := byID[t.TradeID]
		if sv := sellerValue[t.SellerID]; sv > 0 {
			all[idx].Levy = levy[t.SellerID] * o.Escrow / sv
		}
		// Community benefit on the energy the pool confirmed: the seller beats the
		// feed-in price, the buyer beats retail.
		confirmed := t.Received() * o.Fill
		if s := homes[t.SellerID]; s != nil {
			s.savings += (t.Price - FeedInPrice) * confirmed
		}
		if b := homes[t.BuyerID]; b != nil {
			b.savings += (RetailPrice - t.Price) * confirmed
		}
	}
}

// fairness returns total community savings and the Gini coefficient of per-home savings.
func (e *Engine) fairness() (total, gini float64) {
	var xs []float64
	for _, hs := range e.homes {
		for _, h := range hs {
			xs = append(xs, math.Max(0, h.savings))
			total += h.savings
		}
	}
	return total, Gini(xs)
}

// Gini returns the Gini coefficient of non-negative values (0 = perfectly equal).
func Gini(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	var cum, sum float64
	for i, v := range s {
		cum += float64(i+1) * v
		sum += v
	}
	if sum == 0 {
		return 0
	}
	return (2*cum)/(float64(n)*sum) - float64(n+1)/float64(n)
}

// ---- queries ----

// Latest returns the most recent interval, if any.
func (e *Engine) Latest() (IntervalRecord, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.last, e.hasLast
}

// ProofFor returns the Merkle inclusion proof for a recent trade.
func (e *Engine) ProofFor(tradeID string) (Proof, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ri, ok := e.tradeRoot[tradeID]
	if !ok {
		return Proof{}, false
	}
	s := e.roots[ri]
	idx := -1
	for i, id := range s.tradeIDs {
		if id == tradeID {
			idx = i
		}
	}
	if idx < 0 {
		return Proof{}, false
	}
	steps := settlement.MerkleProof(s.leaves, idx)
	return Proof{
		TradeID: tradeID, Leaf: s.leaves[idx], Root: s.root, Steps: steps,
		Valid: settlement.VerifyProof(s.leaves[idx], steps, s.root), Trade: e.tradeLog[tradeID],
	}, true
}

// Balances returns every wallet plus the solidarity pool.
func (e *Engine) Balances() (map[string]float64, float64) {
	return e.ledger.Snapshot()
}

// Run advances the market every cfg.Tick until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(e.cfg.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rec := e.Step()
			if e.publish != nil {
				e.publish(rec)
			}
			if e.persist != nil {
				e.persist(rec)
			}
		}
	}
}
