package telemetrystore

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
)

// Flusher writes one batch. Store.Insert is the real one.
type Flusher func(ctx context.Context, batch []models.TelemetryEvent) error

// Sink batches events and flushes when the batch is full or on a timer, so the database sees a
// few large COPY writes per second instead of one INSERT per event. It satisfies ingestion.Sink.
type Sink struct {
	in      chan models.TelemetryEvent
	flush   Flusher
	size    int
	every   time.Duration
	done    chan struct{}
	once    sync.Once
	mu      sync.RWMutex
	closed  bool
	Dropped uint64 // events lost because a flush failed
}

// NewSink starts the background flusher. size is the batch size and every the longest an
// event waits before being written.
func NewSink(flush Flusher, size int, every time.Duration) *Sink {
	s := &Sink{in: make(chan models.TelemetryEvent, size*4), flush: flush, size: size, every: every, done: make(chan struct{})}
	go s.loop()
	return s
}

func (s *Sink) Name() string { return "timescale" }

// Write enqueues an event. It blocks when the buffer is full (back-pressure) and fails after Close.
func (s *Sink) Write(ctx context.Context, ev models.TelemetryEvent) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return errors.New("telemetry sink closed")
	}
	select {
	case s.in <- ev:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Sink) loop() {
	defer close(s.done)
	batch := make([]models.TelemetryEvent, 0, s.size)
	t := time.NewTicker(s.every)
	defer t.Stop()
	write := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.flush(context.Background(), batch); err != nil {
			s.Dropped += uint64(len(batch))
			log.Printf("telemetry flush of %d events failed: %v", len(batch), err)
		}
		batch = batch[:0]
	}
	for {
		select {
		case ev, ok := <-s.in:
			if !ok {
				write()
				return
			}
			batch = append(batch, ev)
			if len(batch) >= s.size {
				write()
			}
		case <-t.C:
			write()
		}
	}
}

// Close flushes what is buffered and stops the sink.
func (s *Sink) Close() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		close(s.in)
		s.mu.Unlock()
		<-s.done
	})
}
