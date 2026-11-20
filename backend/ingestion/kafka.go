// Package ingestion moves node telemetry from the in-process buffer to its destinations.
//
// Events enter TelemetryStream (from simulated nodes or the MQTT bridge). A pool of
// workers hands every event to every Sink: Kafka for the arbiter, Redis for live fleet
// state, TimescaleDB for history. A slow or failing sink never stops the others, and
// failures are counted per sink.
package ingestion

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
	"github.com/segmentio/kafka-go"
)

// TelemetryStream is the buffer. It can hold 20,000 events before it starts blocking.
var TelemetryStream = make(chan models.TelemetryEvent, 20000)

// Sink is a destination for telemetry.
type Sink interface {
	Name() string
	Write(ctx context.Context, ev models.TelemetryEvent) error
}

// KafkaSink publishes events to a Kafka topic. NodeID is the message key, so all events from
// one node land in one partition and keep their order.
type KafkaSink struct{ w *kafka.Writer }

func NewKafkaSink(brokers []string, topic string) *KafkaSink {
	return &KafkaSink{w: &kafka.Writer{
		Addr: kafka.TCP(brokers...), Topic: topic, Balancer: &kafka.LeastBytes{},
		Async:                  true, // throughput over per-message acknowledgement
		AllowAutoTopicCreation: true, // the topic must exist; create it on first write if it does not
	}}
}

func (k *KafkaSink) Name() string { return "kafka" }

func (k *KafkaSink) Write(ctx context.Context, ev models.TelemetryEvent) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return k.w.WriteMessages(ctx, kafka.Message{Key: []byte(ev.NodeID), Value: payload})
}

func (k *KafkaSink) Close() error { return k.w.Close() }

// Pipeline is a running worker pool.
type Pipeline struct {
	sinks     []Sink
	processed atomic.Int64
	errs      []atomic.Int64
	lastLog   []atomic.Int64
	wg        sync.WaitGroup
}

// StartWorkers starts n workers reading TelemetryStream and writing each event to every sink.
func StartWorkers(n int, sinks ...Sink) *Pipeline {
	return startWorkers(TelemetryStream, n, sinks...)
}

func startWorkers(in <-chan models.TelemetryEvent, n int, sinks ...Sink) *Pipeline {
	p := &Pipeline{sinks: sinks, errs: make([]atomic.Int64, len(sinks)), lastLog: make([]atomic.Int64, len(sinks))}
	for i := 0; i < n; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			ctx := context.Background()
			for ev := range in {
				for si, s := range p.sinks {
					if err := s.Write(ctx, ev); err != nil {
						p.errs[si].Add(1)
						p.logErr(si, err)
					}
				}
				p.processed.Add(1)
			}
		}()
	}
	return p
}

// logErr prints at most one line per second per sink.
func (p *Pipeline) logErr(si int, err error) {
	now := time.Now().UnixMilli()
	if last := p.lastLog[si].Load(); now-last > 1000 && p.lastLog[si].CompareAndSwap(last, now) {
		log.Printf("ingestion: sink %s error (%d so far): %v", p.sinks[si].Name(), p.errs[si].Load(), err)
	}
}

// Processed is the number of events handed to all sinks.
func (p *Pipeline) Processed() int64 { return p.processed.Load() }

// Errors is the number of failed writes for the named sink.
func (p *Pipeline) Errors(name string) int64 {
	for i, s := range p.sinks {
		if s.Name() == name {
			return p.errs[i].Load()
		}
	}
	return 0
}

// Wait blocks until the input channel is closed and drained (used in tests).
func (p *Pipeline) Wait() { p.wg.Wait() }

// StartWorkerPool is the original Kafka-only entry point.
func StartWorkerPool(numWorkers int, brokers []string, topic string) {
	StartWorkers(numWorkers, NewKafkaSink(brokers, topic))
}
