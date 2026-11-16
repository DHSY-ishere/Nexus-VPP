package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/db"
	"github.com/DHSY-ishere/Nexus-VPP/backend/engine"
	"github.com/DHSY-ishere/Nexus-VPP/backend/ingestion"
	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
	"github.com/gorilla/websocket"
	"github.com/segmentio/kafka-go"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan any
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	commands   chan map[string]any
	mu         sync.Mutex
}

var chainMu sync.Mutex

func processTrade(trade *models.P2PTrade) {
	chainMu.Lock()
	defer chainMu.Unlock()
	var lastTrade models.P2PTrade
	res := db.DB.Order("block_index desc").First(&lastTrade)

	var prevHash string
	if res.Error != nil {
		prevHash = "GENESIS"
		trade.BlockIndex = 0
	} else {
		prevHash = lastTrade.BlockHash
		trade.BlockIndex = lastTrade.BlockIndex + 1
	}

	models.MineBlock(trade, prevHash)
	db.DB.Create(trade)
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan any),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		commands:   make(chan map[string]any),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			delete(h.clients, client)
			client.Close()
			h.mu.Unlock()
		case event := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				if err := client.WriteJSON(event); err != nil {
					client.Close()
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

func main() {
	hub := newHub()
	go hub.run()

	// Grid-aware P2P market (L2 agents, L4 market, L5 settlement, L6 twin).
	eng := engine.New(engine.DefaultConfig(), func(v any) { hub.broadcast <- v }, persistInterval)

	// Override state for sabotage simulation
	var (
		overrideMu            sync.Mutex
		frequencyOverride     *float64
		replenishmentOverride *float64
		byzantineActive       bool
		stressTestActive      bool
		messagesProcessed     int64
	)

	// Kafka workers
	brokers := []string{"127.0.0.1:9092"}
	ingestion.StartWorkerPool(50, brokers, "nexus.telemetry.raw")

	// Command handler
	go func() {
		for command := range hub.commands {
			overrideMu.Lock()
			if cmd, ok := command["type"].(string); ok {
				switch cmd {
				case "OVERRIDE":
					if field, ok := command["field"].(string); ok {
						if value, ok := command["value"].(float64); ok {
							switch field {
							case "frequency":
								frequencyOverride = &value
							case "replenishment_rate":
								replenishmentOverride = &value
							}
						}
					}
				case "BYZANTINE_FAULT":
					byzantineActive = true
				case "STRESS_TEST":
					stressTestActive = true
				case "CLEAR_FAULT":
					frequencyOverride = nil
					replenishmentOverride = nil
					byzantineActive = false
					stressTestActive = false
				}
			}
			overrideMu.Unlock()
		}
	}()

	// System Health Broadcast
	go func() {
		for {
			time.Sleep(1 * time.Second)
			overrideMu.Lock()
			msgs := messagesProcessed
			messagesProcessed = 0
			overrideMu.Unlock()

			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			hub.broadcast <- map[string]any{
				"type":       "SYSTEM_HEALTH",
				"throughput": msgs,
				"memory_mb":  m.Alloc / 1024 / 1024,
				"goroutines": runtime.NumGoroutine(),
			}
		}
	}()

	// Mock Trade Generator for Presentation if ML is slow
	go func() {
		for {
			time.Sleep(2 * time.Second)
			overrideMu.Lock()
			freq := 50.0
			if frequencyOverride != nil {
				freq = *frequencyOverride
			} else if replenishmentOverride != nil && *replenishmentOverride == 0 {
				freq = 49.1
			}
			overrideMu.Unlock()

			if freq < 49.9 {
				// Provisioning Criteria (Peak vs. Non-Peak / Daylight vs. Moonlight)
				hour := time.Now().Hour()
				isDaylight := hour >= 6 && hour < 18
				isPeak := hour >= 10 && hour < 16

				// Mock telemetry data for the simulated trade
				consumption := rand.Float64() * 10
				replenishment := rand.Float64() * 8
				battery := 20.0 + rand.Float64()*80

				// Peak Replenishment Period & Consumer Threshold Calculations
				threshold := 50.0
				multiplier := 0.10
				if isPeak {
					threshold = 40.0
					multiplier = 0.15
				} else if !isDaylight {
					threshold = 60.0
					multiplier = 0.05
				}

				if battery > threshold {
					// Dynamic Storage Energy Contribution Calculation
					batteryContribution := battery * multiplier
					netEnergy := replenishment - consumption

					var energyKw float64
					if netEnergy > 0 {
						energyKw = batteryContribution + (netEnergy * 0.8)
					} else {
						energyKw = batteryContribution + netEnergy
						if energyKw < 1.0 {
							energyKw = 1.0
						}
					}

					loss := energyKw * 0.06 // 6% I^2R loss

					tradeMsg := map[string]any{
						"type":            "TRADE_EVENT",
						"trade_id":        fmt.Sprintf("TRD-%d", rand.Intn(99999)),
						"contributor_id":  fmt.Sprintf("node_%d", rand.Intn(500)),
						"consumer_id":     fmt.Sprintf("node_%d", rand.Intn(500)),
						"energy_kw":       energyKw,
						"energy_received": energyKw - loss,
						"loss_kw":         loss,
						"price_kwh":       0.82 + rand.Float64()*0.2,
						"credit":          (energyKw - loss) * 0.82,
					}

					trade := models.P2PTrade{
						TradeID:          tradeMsg["trade_id"].(string),
						ContributorID:    tradeMsg["contributor_id"].(string),
						EnergyProvidedKw: tradeMsg["energy_kw"].(float64),
						CreditEarned:     tradeMsg["credit"].(float64),
						GridFrequency:    freq,
						SettledAt:        time.Now(),
					}
					processTrade(&trade)
					tradeMsg["block_hash"] = trade.BlockHash
					tradeMsg["block_index"] = trade.BlockIndex

					hub.broadcast <- tradeMsg
				}
			}
		}
	}()

	// Ingestion + broadcast loop
	go func() {
		for {
			overrideMu.Lock()
			var freq float64
			var replenish float64
			isByzantine := byzantineActive
			isStress := stressTestActive
			if frequencyOverride != nil {
				freq = *frequencyOverride
			} else if replenishmentOverride != nil && *replenishmentOverride == 0 {
				freq = 49.1 + (rand.Float64()-0.5)*0.1
			} else {
				freq = 50.0 + (rand.Float64()-0.5)*0.1
			}
			if replenishmentOverride != nil {
				replenish = *replenishmentOverride
			} else {
				replenish = rand.Float64() * 8
			}
			overrideMu.Unlock()

			batchSize := 1
			if isStress {
				batchSize = 50 // Ramp up throughput
			}

			for i := 0; i < batchSize; i++ {
				nodeID := fmt.Sprintf("node_%d", rand.Intn(5000))
				nodeFreq := freq

				// Inject Malicious Data
				if isByzantine && rand.Float64() > 0.95 {
					nodeFreq = 60.0 // Impossible frequency
					hub.broadcast <- map[string]any{
						"type":    "ANOMALY_DETECTED",
						"node_id": nodeID,
					}
				}

				event := models.TelemetryEvent{
					NodeID:             nodeID,
					NodeType:           "PROSUMER",
					Timestamp:          time.Now(),
					CurrentConsumption: rand.Float64() * 10,
					ReplenishmentRate:  replenish,
					StateOfCharge:      20.0 + rand.Float64()*80, // Prevent dropping below 20% DOD
					LocalFrequency:     nodeFreq,
					Voltage:            230.0 + (rand.Float64()-0.5)*5,
				}
				ingestion.TelemetryStream <- event

				// Only broadcast a sample to UI to prevent crashing browser, but process all
				if i == 0 {
					hub.broadcast <- event
				}

				overrideMu.Lock()
				messagesProcessed++
				overrideMu.Unlock()
			}

			if isStress {
				time.Sleep(10 * time.Millisecond)
			} else {
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	db.InitDB()
	go eng.Run(context.Background())

	go func() {
		reader := kafka.NewReader(kafka.ReaderConfig{
			Brokers: []string{"127.0.0.1:9092"},
			Topic:   "nexus.commands",
			GroupID: "go-orchestrator-settlement",
		})
		defer reader.Close()

		for {
			msg, err := reader.ReadMessage(context.Background())
			if err != nil {
				continue
			}

			var command map[string]any
			if err := json.Unmarshal(msg.Value, &command); err == nil {
				if command["type"] == "EXECUTE_TRADE" {
					trade := models.P2PTrade{
						TradeID:          command["trade_id"].(string),
						ContributorID:    command["contributor_id"].(string),
						EnergyProvidedKw: command["energy_kw"].(float64),
						CreditEarned:     command["credit"].(float64),
						GridFrequency:    command["grid_frequency"].(float64),
						SettledAt:        time.Now(),
					}
					processTrade(&trade)
					command["block_hash"] = trade.BlockHash
					command["block_index"] = trade.BlockIndex
					hub.broadcast <- command // Forward ML trades to UI
				}
			}
		}
	}()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		hub.register <- conn
		go func(c *websocket.Conn) {
			for {
				var msg map[string]any
				if err := c.ReadJSON(&msg); err != nil {
					hub.unregister <- c
					return
				}
				if _, ok := msg["type"].(string); ok {
					hub.commands <- msg
				}
			}
		}(conn)
	})

	http.HandleFunc("/chain/verify", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		var trades []models.P2PTrade
		db.DB.Order("block_index asc").Find(&trades)

		valid := true
		var errors []string

		for i, trade := range trades {
			expectedHash := models.ComputeBlockHash(trade)
			if expectedHash != trade.BlockHash {
				valid = false
				errors = append(errors, fmt.Sprintf("Block %d invalid hash", trade.BlockIndex))
			}
			if i > 0 {
				if trade.PreviousHash != trades[i-1].BlockHash {
					valid = false
					errors = append(errors, fmt.Sprintf("Block %d invalid previous hash", trade.BlockIndex))
				}
			} else {
				if trade.PreviousHash != "GENESIS" {
					valid = false
					errors = append(errors, fmt.Sprintf("Genesis block invalid previous hash"))
				}
			}
		}

		if errors == nil {
			errors = make([]string, 0)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"valid":  valid,
			"blocks": len(trades),
			"errors": errors,
		})
	})

	http.HandleFunc("/chain/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		var trades []models.P2PTrade
		db.DB.Order("block_index desc").Limit(20).Find(&trades)

		var result []map[string]any
		for _, trade := range trades {
			result = append(result, map[string]any{
				"block_index":    trade.BlockIndex,
				"trade_id":       trade.TradeID,
				"contributor_id": trade.ContributorID,
				"energy_kw":      trade.EnergyProvidedKw,
				"credit":         trade.CreditEarned,
				"block_hash":     trade.BlockHash,
				"previous_hash":  trade.PreviousHash,
				"settled_at":     trade.SettledAt,
			})
		}
		if result == nil {
			result = make([]map[string]any, 0)
		}
		json.NewEncoder(w).Encode(result)
	})

	registerMarketRoutes(eng)

	fmt.Println("Nexus Orchestrator live on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
