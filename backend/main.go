package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/ingestion"
	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
)

func main() {
	fmt.Println("⚡ Starting Nexus VPP Ingestion Engine...")

	// 1. Start 50 Kafka workers listening to the stream
	brokers := []string{"localhost:9092"}
	topic := "nexus.telemetry.raw"
	ingestion.StartWorkerPool(50, brokers, topic)

	fmt.Println("🟢 Worker pool active. Igniting mock nodes...")

	// 2. Simulate 5000 nodes pinging every second
	nodeTypes := []string{"CONTRIBUTOR", "CONSUMER", "PROSUMER"}

	for {
		start := time.Now()
		dropped := 0

		for i := 0; i < 5000; i++ {
			nodeID := fmt.Sprintf("node_%d", i)
			nType := nodeTypes[i%3]

			// Generate fake telemetry
			event := models.TelemetryEvent{
				NodeID:             nodeID,
				NodeType:           nType,
				Timestamp:          time.Now(),
				CurrentConsumption: rand.Float64() * 10,                 // 0-10 kW
				ReplenishmentRate:  rand.Float64() * 5,                  // 0-5 kW (solar/wind)
				StateOfCharge:      rand.Float64() * 100,                // 0-100% battery
				LocalFrequency:     50.0 + (rand.Float64()-0.5)*0.2,     // Micro-fluctuations around 50Hz
				Voltage:            230.0 + (rand.Float64()-0.5)*5,      // 225-235V range
			}

			// Non-blocking send to the stream
			select {
			case ingestion.TelemetryStream <- event:
			default:
				dropped++ // If channel is full, drop the packet (simulates network packet loss backpressure)
			}
		}
		
		elapsed := time.Since(start)
		fmt.Printf("📡 Blasted 5000 events to Kafka in %s. Dropped: %d\n", elapsed, dropped)
		
		// Wait the remainder of the second before the next wave
		time.Sleep(time.Second - elapsed)
	}
}
