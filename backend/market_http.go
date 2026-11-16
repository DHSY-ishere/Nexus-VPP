package main

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/DHSY-ishere/Nexus-VPP/backend/db"
	"github.com/DHSY-ishere/Nexus-VPP/backend/engine"
	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// persistInterval stores one interval's trades and its Merkle root.
func persistInterval(rec engine.IntervalRecord) {
	if db.DB == nil {
		return
	}
	rows := make([]models.MarketTrade, 0, len(rec.Trades))
	for _, t := range rec.Trades {
		rows = append(rows, models.MarketTrade{
			TradeID: t.TradeID, IntervalStart: t.IntervalStart, ZoneID: t.Zone,
			SellerID: t.SellerID, BuyerID: t.BuyerID, EnergyKWh: t.EnergyKWh,
			ClearingPrice: t.Price, LossFactor: t.LossFactor, GateResult: t.GateResult,
			MeasuredKWh: t.MeasuredKWh, StabilityCredit: t.StabilityCredit, Levy: t.Levy,
			Status: t.Status, LeafHash: t.LeafHash, MerkleRoot: rec.MerkleRoot,
		})
	}
	if len(rows) > 0 {
		db.DB.Create(&rows)
	}
	db.DB.Create(&models.IntervalRoot{
		IntervalStart: rec.Interval, Root: rec.MerkleRoot, TradeCount: len(rows), Mode: rec.Mode,
	})
}

func registerMarketRoutes(eng *engine.Engine) {
	// Latest interval, solidarity pool, and the richest and poorest wallets.
	http.HandleFunc("/market/state", func(w http.ResponseWriter, r *http.Request) {
		rec, ok := eng.Latest()
		wallets, pool := eng.Balances()
		type kv struct {
			ID      string  `json:"id"`
			Balance float64 `json:"balance"`
		}
		var top []kv
		for id, b := range wallets {
			top = append(top, kv{id, b})
		}
		sort.Slice(top, func(i, j int) bool { return top[i].Balance > top[j].Balance })
		if len(top) > 10 {
			top = top[:10]
		}
		writeJSON(w, http.StatusOK, map[string]any{"ready": ok, "latest": rec, "pool": pool, "top_wallets": top})
	})

	// Merkle inclusion proof for a recent trade: ?trade_id=Z1-0101T1200-001
	http.HandleFunc("/market/proof", func(w http.ResponseWriter, r *http.Request) {
		p, ok := eng.ProofFor(r.URL.Query().Get("trade_id"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "trade not found or no longer retained"})
			return
		}
		writeJSON(w, http.StatusOK, p)
	})
}
