package ingestion

import (
	"context"
	"encoding/json"
	"log"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
	"github.com/segmentio/kafka-go"
)

// TelemetryStream is the buffer. It can hold 20,000 events before it starts blocking.
var TelemetryStream = make(chan models.TelemetryEvent, 20000)

// StartWorkerPool initializes the Kafka writer and starts background workers
func StartWorkerPool(numWorkers int, brokers []string, topic string) {
	writer := &kafka.Writer{
		Addr:     kafka.TCP(brokers...),
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},
		// Async mode for massive throughput
		Async: true, 
	}

	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			for event := range TelemetryStream {
				// 1. Serialize to JSON
				payload, err := json.Marshal(event)
				if err != nil {
					log.Printf("Worker %d JSON error: %v", workerID, err)
					continue
				}

				// 2. Write to Kafka. We use NodeID as the Key.
				// *Production Best Practice*: Using the NodeID as the key ensures all events 
				// from a specific node always go to the exact same Kafka partition. 
				// This guarantees strict chronological order for that node's data.
				err = writer.WriteMessages(context.Background(),
					kafka.Message{
						Key:   []byte(event.NodeID), 
						Value: payload,
					},
				)
				if err != nil {
					log.Printf("Worker %d Kafka write error: %v", workerID, err)
				}
			}
		}(i)
	}
}