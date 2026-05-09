package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/db"
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
	mu         sync.Mutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan any),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
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

	// Kafka workers
	brokers := []string{"127.0.0.1:9092"}
	ingestion.StartWorkerPool(50, brokers, "nexus.telemetry.raw")

	// Ingestion + broadcast loop
	go func() {
		for {
			event := models.TelemetryEvent{
				NodeID:             fmt.Sprintf("node_%d", rand.Intn(5000)),
				NodeType:           "PROSUMER",
				Timestamp:          time.Now(),
				CurrentConsumption: rand.Float64() * 10,
				ReplenishmentRate:  rand.Float64() * 8,
				StateOfCharge:      rand.Float64() * 100,
				LocalFrequency:     50.0 + (rand.Float64()-0.5)*0.2,
				Voltage:            230.0 + (rand.Float64()-0.5)*5,
			}
			ingestion.TelemetryStream <- event
			hub.broadcast <- event
			time.Sleep(100 * time.Millisecond)
		}
	}()

	db.InitDB()

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
				log.Printf("command read error: %v", err)
				continue
			}

			var command map[string]any
			if err := json.Unmarshal(msg.Value, &command); err != nil {
				log.Printf("command decode error: %v", err)
				continue
			}

			if command["type"] == "EXECUTE_TRADE" {
				tradeID, okTrade := command["trade_id"].(string)
				contributorID, okContributor := command["contributor_id"].(string)
				energyKw, okEnergy := command["energy_kw"].(float64)
				credit, okCredit := command["credit"].(float64)
				freq, okFreq := command["freq"].(float64)
				if !okTrade || !okContributor || !okEnergy || !okCredit || !okFreq {
					log.Printf("invalid trade command payload: %v", command)
					continue
				}

				trade := models.P2PTrade{
					TradeID:          tradeID,
					ContributorID:    contributorID,
					EnergyProvidedKw: energyKw,
					CreditEarned:     credit,
					GridFrequency:    freq,
					SettledAt:        time.Now(),
				}

				if err := db.DB.Create(&trade).Error; err != nil {
					log.Printf("failed to persist trade: %v", err)
					continue
				}

				hub.broadcast <- command
			}
		}
	}()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("websocket upgrade error: %v", err)
			return
		}

		hub.register <- conn

		// Keep this connection alive until client disconnects.
		go func(c *websocket.Conn) {
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					hub.unregister <- c
					return
				}
			}
		}(conn)
	})

	fmt.Println("Nexus Orchestrator live on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
