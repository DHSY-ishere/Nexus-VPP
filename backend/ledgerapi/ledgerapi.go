// Package ledgerapi serves the settlement blockchain over HTTP, shared by the full
// orchestrator and the market-only demo.
//
//	GET /ledger/info                       validators, quorum, height, head hash
//	GET /ledger/verify                     full audit: links, signatures, re-execution of every block
//	GET /ledger/block?height=N | latest    one block with its evidence
//	GET /ledger/delivery?trade_id=ID       proof of receipt for a trade
package ledgerapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/chain"
	"github.com/DHSY-ishere/Nexus-VPP/backend/delivery"
	"github.com/DHSY-ishere/Nexus-VPP/backend/engine"
	"github.com/DHSY-ishere/Nexus-VPP/backend/market"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Register adds the /ledger routes to the default mux.
func Register(eng *engine.Engine) {
	need := func(w http.ResponseWriter) *chain.Chain {
		c := eng.Chain()
		if c == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "blockchain disabled"})
		}
		return c
	}

	http.HandleFunc("/ledger/info", func(w http.ResponseWriter, r *http.Request) {
		if c := need(w); c != nil {
			writeJSON(w, http.StatusOK, c.Info())
		}
	})

	http.HandleFunc("/ledger/verify", func(w http.ResponseWriter, r *http.Request) {
		c := need(w)
		if c == nil {
			return
		}
		t0 := time.Now()
		err := c.Verify()
		info := c.Info()
		out := map[string]any{"valid": err == nil, "blocks": info.Blocks, "height": info.Height,
			"head_hash": info.HeadHash, "quorum": info.Quorum, "validators": len(info.Validators),
			"audit_ms": float64(time.Since(t0).Microseconds()) / 1000}
		if err != nil {
			out["error"] = err.Error()
		}
		writeJSON(w, http.StatusOK, out)
	})

	http.HandleFunc("/ledger/block", func(w http.ResponseWriter, r *http.Request) {
		c := need(w)
		if c == nil {
			return
		}
		q := r.URL.Query().Get("height")
		var b chain.Block
		var ok bool
		if q == "" || q == "latest" {
			b, ok = c.Latest()
		} else if h, err := strconv.ParseUint(q, 10, 64); err == nil {
			b, ok = c.Block(h)
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "block not found or no longer retained"})
			return
		}
		writeJSON(w, http.StatusOK, b)
	})

	http.HandleFunc("/ledger/delivery", func(w http.ResponseWriter, r *http.Request) {
		c := need(w)
		if c == nil {
			return
		}
		id := r.URL.Query().Get("trade_id")
		b, zr, ok := c.FindTrade(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "trade not found or no longer retained"})
			return
		}
		writeJSON(w, http.StatusOK, proof(b, zr, id, c.Quorum()))
	})
}

// DeliveryProof is what a buyer receives to check that it got what it paid for.
type DeliveryProof struct {
	TradeID  string                `json:"trade_id"`
	Verdict  string                `json:"verdict"`
	Block    any                   `json:"block"`
	Trade    market.Trade          `json:"trade"`
	Outcome  delivery.TradeOutcome `json:"outcome"`
	Buyer    *Evidence             `json:"buyer"`
	Seller   *Evidence             `json:"seller"`
	Zone     map[string]any        `json:"zone"`
	Evidence string                `json:"how_to_check"`
}

// Evidence is one party's signed meter reading against its baseline.
type Evidence struct {
	Party    delivery.Party `json:"party"`
	Baseline float64        `json:"baseline_kwh"`
	Reading  float64        `json:"reading_kwh"`
	Device   string         `json:"device"`
	Sig      []byte         `json:"sig"`
}

func proof(b chain.Block, zr chain.ZoneRecord, id string, quorum int) DeliveryProof {
	p := DeliveryProof{TradeID: id}
	for _, t := range chain.FinalTrades(zr) {
		if t.TradeID == id {
			p.Trade = t
		}
	}
	for _, o := range zr.Result.Trades {
		if o.TradeID == id {
			p.Outcome = o
		}
	}
	ev := func(partyID, role string) *Evidence {
		e := &Evidence{}
		for _, pt := range zr.Result.Parties {
			if pt.ID == partyID && pt.Role == role {
				e.Party = pt
			}
		}
		for _, h := range zr.Input.Homes {
			if h.ID == partyID {
				e.Baseline, e.Reading, e.Device, e.Sig = h.BaselineKWh, h.Reading.NetKWh, h.Reading.DeviceID, h.Reading.Sig
			}
		}
		return e
	}
	p.Buyer, p.Seller = ev(p.Trade.BuyerID, "BUYER"), ev(p.Trade.SellerID, "SELLER")
	res := zr.Result
	p.Zone = map[string]any{
		"zone": zr.Input.Zone, "conserved": res.Conserved, "homes_net_kwh": res.SumHomesKWh,
		"loss_kwh": res.LossKWh, "head_meter_kwh": res.HeadKWh, "excess_kwh": res.ExcessKWh,
		"tolerance_kwh": res.ToleranceKWh, "claimed_usd": claimed(res), "confirmed_usd": res.MatchedValue,
		"note": res.Note,
	}
	p.Block = map[string]any{
		"height": b.Height, "hash": b.Hash, "prev_hash": b.PrevHash, "proposer": b.Proposer,
		"signatures": len(b.Sigs), "quorum": quorum, "trade_root": b.TradeRoot,
	}
	p.Verdict = verdict(p)
	p.Evidence = "Every validator re-ran the same rules on this evidence before signing. " +
		"Fetch /ledger/verify to audit the whole chain, or /ledger/block?height=" + strconv.FormatUint(b.Height, 10) + " for the raw evidence."
	return p
}

func claimed(r delivery.Result) float64 {
	var v float64
	for _, t := range r.Trades {
		v += t.Escrow
	}
	return v
}

func verdict(p DeliveryProof) string {
	pct := p.Outcome.Fill * 100
	switch {
	case p.Outcome.Status == delivery.StatusSettled:
		return fmt.Sprintf("DELIVERED: buyer's draw and seller's injection are both confirmed, and the zone's meters add up to the independent feeder-head meter. %.0f%% charged.", pct)
	case p.Outcome.Status == delivery.StatusDisputed:
		return "NOT CONFIRMED: nothing could be verified for this trade, buyer fully refunded."
	default:
		return fmt.Sprintf("PARTLY CONFIRMED: only %.0f%% of this payment was released; the rest was refunded. Power cannot be routed to one buyer, so any shortfall in the zone is shared across all buyers pro rata.", pct)
	}
}
