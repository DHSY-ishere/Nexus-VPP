// Command marketdemo runs only the grid-aware market engine, with no Kafka or
// Postgres. It serves the same /ws, /market/state and /market/proof endpoints as the
// full orchestrator, so the frontend works against either. Use it for demos and
// quick checks: `go run ./cmd/marketdemo` (set PORT to change :8080, TICK_MS to change speed).
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/engine"
	"github.com/DHSY-ishere/Nexus-VPP/backend/ingestion"
	"github.com/DHSY-ishere/Nexus-VPP/backend/integrations"
	"github.com/DHSY-ishere/Nexus-VPP/backend/ledgerapi"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]bool
}

func (h *hub) send(v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if err := c.WriteJSON(v); err != nil {
			c.Close()
			delete(h.clients, c)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func main() {
	h := &hub{clients: map[*websocket.Conn]bool{}}
	cfg := engine.DefaultConfig()
	if ms, err := strconv.Atoi(os.Getenv("TICK_MS")); err == nil && ms > 0 {
		cfg.Tick = time.Duration(ms) * time.Millisecond
	}
	eng := engine.New(cfg, h.send, nil)
	go eng.Run(context.Background())

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		h.mu.Lock()
		h.clients[c] = true
		h.mu.Unlock()
		if rec, ok := eng.Latest(); ok { // new viewers see data immediately
			c.WriteJSON(rec)
		}
		go func() {
			for {
				var msg map[string]any
				if err := c.ReadJSON(&msg); err != nil {
					h.mu.Lock()
					delete(h.clients, c)
					h.mu.Unlock()
					c.Close()
					return
				}
				switch msg["type"] {
				case "MARKET_ISLAND":
					v, _ := msg["value"].(bool)
					eng.SetIsland(v)
				case "MARKET_CLOUD":
					if v, ok := msg["value"].(float64); ok {
						eng.SetCloud(v)
					}
				case "MARKET_TAMPER":
					v, _ := msg["value"].(bool)
					eng.SetTamper(v)
				case "MARKET_METERLIE":
					v, _ := msg["value"].(bool)
					eng.SetMeterLie(v)
				case "CLEAR_FAULT":
					eng.ClearFaults()
				}
			}
		}()
	})

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

	http.HandleFunc("/market/proof", func(w http.ResponseWriter, r *http.Request) {
		p, ok := eng.ProofFor(r.URL.Query().Get("trade_id"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "trade not found or no longer retained"})
			return
		}
		writeJSON(w, http.StatusOK, p)
	})

	ledgerapi.Register(eng)
	services, sinks := integrations.Start(context.Background(), integrations.FromEnv(), eng, http.DefaultServeMux)
	defer services.Close()
	// No Kafka here: telemetry that arrives over MQTT goes straight to Redis and TimescaleDB.
	ingestion.StartWorkers(8, sinks...)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Nexus market demo (no Kafka/Postgres) on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
