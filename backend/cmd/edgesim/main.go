// Command edgesim simulates edge nodes publishing telemetry to an MQTT broker, the way real
// home agents would.
//
//	go run ./cmd/edgesim -broker tcp://127.0.0.1:1883 -nodes 200 -hz 1 -seconds 10
//	go run ./cmd/edgesim -nodes 50 -freq 49.4        # a strained grid
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
	"github.com/DHSY-ishere/Nexus-VPP/backend/mqttbridge"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func main() {
	broker := flag.String("broker", "tcp://127.0.0.1:1883", "MQTT broker")
	nodes := flag.Int("nodes", 100, "number of simulated nodes")
	hz := flag.Float64("hz", 1, "messages per node per second")
	seconds := flag.Int("seconds", 10, "how long to run")
	freq := flag.Float64("freq", 50.0, "grid frequency the nodes report")
	flag.Parse()

	opts := mqtt.NewClientOptions().AddBroker(*broker).SetClientID(fmt.Sprintf("edgesim-%d", time.Now().UnixNano()))
	c := mqtt.NewClient(opts)
	if tok := c.Connect(); !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		log.Fatalf("connect %s: %v", *broker, tok.Error())
	}
	defer c.Disconnect(500)

	var sent, failed atomic.Int64
	var wg sync.WaitGroup
	stop := time.Now().Add(time.Duration(*seconds) * time.Second)
	interval := time.Duration(float64(time.Second) / *hz)
	start := time.Now()
	for i := 0; i < *nodes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(i)))
			id := fmt.Sprintf("home-%04d", i)
			soc := 20 + rng.Float64()*70
			time.Sleep(time.Duration(rng.Int63n(int64(interval)))) // de-synchronise the nodes
			for time.Now().Before(stop) {
				soc = clamp(soc+rng.NormFloat64()*0.5, 0, 100)
				ev := models.TelemetryEvent{
					NodeID: id, NodeType: "PROSUMER", Timestamp: time.Now().UTC(),
					CurrentConsumption: 0.4 + rng.Float64()*2, ReplenishmentRate: rng.Float64() * 4,
					StateOfCharge: soc, LocalFrequency: *freq + rng.NormFloat64()*0.02, Voltage: 230 + rng.NormFloat64(),
				}
				raw, _ := json.Marshal(ev)
				if tok := c.Publish(mqttbridge.TelemetryPrefix+id, 1, false, raw); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
					failed.Add(1)
				} else {
					sent.Add(1)
				}
				time.Sleep(interval)
			}
		}(i)
	}
	wg.Wait()
	el := time.Since(start).Seconds()
	fmt.Printf("published %d messages from %d nodes in %.1fs (%.0f msg/s), %d failed\n", sent.Load(), *nodes, el, float64(sent.Load())/el, failed.Load())
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
