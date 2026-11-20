// Package agent is L2: one agent per home. It turns the owner's limits, a local
// forecast and a battery wear model into a bid curve, and it holds pre-sold reflex
// contracts that it can execute from local measurements alone.
package agent

import (
	"math"

	"github.com/DHSY-ishere/Nexus-VPP/backend/market"
)

// Limits are the owner's plain-language rules.
type Limits struct {
	ReserveSoC   float64 // percent the battery must never be sold below
	MinSellPrice float64 // $/kWh, never sell below this
	MaxBuyPrice  float64 // $/kWh, never pay above this
	Critical     bool    // household has life-critical load (served first when islanded)
}

// Agent is the per-home decision maker.
type Agent struct {
	ID             string
	Zone           string
	Bus            int
	CapKWh         float64 // battery capacity
	MaxKW          float64 // inverter limit
	SoC            float64 // percent
	SolarKWp       float64
	BaseLoadKW     float64
	Limits         Limits
	WearCostPerKWh float64 // battery degradation cost of one discharged kWh
	Reflex         *Reflex

	// Demand response, set from an OpenADR event while one is active.
	DRLevel   int     // 0 normal; 1-3 asks the home to shed demand and offer supply
	DRCeiling float64 // $/kWh price signal; the home will not bid above it (0 = none)
}

// DRShiftPerLevel is how many percentage points of state of charge a demand-response level
// lowers the home's target by. A lower target means the home buys less and offers more.
const DRShiftPerLevel = 8.0

// Reflex is a pre-sold response curve: the owner agrees in advance to discharge in
// proportion to how far local frequency or voltage has fallen. The agent acts on
// local measurements and settlement happens afterwards from meter data.
type Reflex struct {
	HzBelow   float64 // starts responding under this frequency
	KWPerHz   float64 // discharge per Hz of deviation
	VoltBelow float64 // or under this voltage
	KWPerVolt float64
	PricePerK float64 // $/kWh paid for reflex energy
}

// Respond returns the discharge power (kW) the agent contributes for the measured
// frequency and voltage. It never goes below the owner's reserve.
func (a *Agent) Respond(hz, volt float64) float64 {
	r := a.Reflex
	if r == nil || a.SoC <= a.Limits.ReserveSoC {
		return 0
	}
	kw := 0.0
	if hz > 0 && hz < r.HzBelow {
		kw += (r.HzBelow - hz) * r.KWPerHz
	}
	if volt > 0 && volt < r.VoltBelow {
		kw += (r.VoltBelow - volt) * r.KWPerVolt
	}
	return math.Min(kw, a.MaxKW)
}

// TargetSoC is the state of charge the agent wants at a given hour: low after the
// night, filling through the solar day, full for the evening peak.
func TargetSoC(hour float64) float64 {
	switch {
	case hour < 6:
		return 35
	case hour < 16:
		return 35 + (hour-6)/10*45 // 35 -> 80
	case hour < 22:
		return 80 - (hour-16)/6*25 // 80 -> 55
	default:
		return 45
	}
}

// Bid builds this home's bid curve for one interval of hoursLong hours.
// ok is false when the agent has nothing to offer or want.
func (a *Agent) Bid(hour, hoursLong float64, island bool) (bid market.Bid, ok bool) {
	band := 6.0
	target := TargetSoC(hour) - DRShiftPerLevel*float64(a.DRLevel)
	bid = market.Bid{AgentID: a.ID, Zone: a.Zone, Bus: a.Bus}

	switch {
	case a.SoC > target+band && a.SoC > a.Limits.ReserveSoC:
		// Seller: energy above the target, never below the owner's reserve.
		avail := math.Min(a.MaxKW*hoursLong, (a.SoC-math.Max(a.Limits.ReserveSoC, target))/100*a.CapKWh)
		if avail <= 0.01 {
			return bid, false
		}
		floor := math.Max(a.Limits.MinSellPrice, a.WearCostPerKWh+0.04)
		bid.Side = market.Sell
		bid.Steps = tranches(avail, floor, 1.12)
	case a.SoC < target-band:
		// Buyer: charge toward the target, never pay above the owner's ceiling.
		need := math.Min(a.MaxKW*hoursLong, (target-a.SoC)/100*a.CapKWh)
		if need <= 0.01 {
			return bid, false
		}
		bid.Side = market.Buy
		maxBuy := a.Limits.MaxBuyPrice
		if a.DRCeiling > 0 && a.DRCeiling < maxBuy {
			maxBuy = a.DRCeiling // an announced price signal caps what the home will pay
		}
		if island && a.Limits.Critical {
			bid.Priority = 2
			bid.Steps = []market.Step{{KWh: need, Price: a.Limits.MaxBuyPrice * 3}}
		} else {
			bid.Steps = tranches(need, maxBuy, 0.88)
		}
	default:
		return bid, false
	}
	return bid, true
}

// tranches splits qty into three steps; each later step moves the price by ratio.
func tranches(qty, price, ratio float64) []market.Step {
	steps := make([]market.Step, 3)
	p := price
	for i := range steps {
		steps[i] = market.Step{KWh: qty / 3, Price: p}
		p *= ratio
	}
	return steps
}
