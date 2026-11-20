package telemetrystore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
)

type capture struct {
	mu      sync.Mutex
	batches []int
	total   int
	fail    bool
}

func (c *capture) flush(_ context.Context, b []models.TelemetryEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return errors.New("db down")
	}
	c.batches = append(c.batches, len(b))
	c.total += len(b)
	return nil
}

func (c *capture) snapshot() (batches []int, total int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.batches...), c.total
}

func TestFlushesWhenBatchIsFull(t *testing.T) {
	c := &capture{}
	s := NewSink(c.flush, 100, time.Hour) // timer never fires
	for i := 0; i < 250; i++ {
		s.Write(context.Background(), models.TelemetryEvent{NodeID: fmt.Sprint(i)})
	}
	s.Close() // flushes the remaining 50
	batches, total := c.snapshot()
	if total != 250 || len(batches) != 3 || batches[0] != 100 || batches[1] != 100 || batches[2] != 50 {
		t.Fatalf("batches %v total %d", batches, total)
	}
}

func TestFlushesOnTimerWhenQuiet(t *testing.T) {
	c := &capture{}
	s := NewSink(c.flush, 1000, 30*time.Millisecond)
	defer s.Close()
	for i := 0; i < 7; i++ {
		s.Write(context.Background(), models.TelemetryEvent{NodeID: "n"})
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, total := c.snapshot(); total == 7 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("quiet batch was never flushed by the timer")
}

func TestWriteAfterCloseFailsAndFlushFailureIsCounted(t *testing.T) {
	c := &capture{fail: true}
	s := NewSink(c.flush, 10, time.Hour)
	for i := 0; i < 10; i++ {
		s.Write(context.Background(), models.TelemetryEvent{})
	}
	s.Close()
	if s.Dropped != 10 {
		t.Fatalf("dropped = %d, want 10", s.Dropped)
	}
	if err := s.Write(context.Background(), models.TelemetryEvent{}); err == nil {
		t.Fatal("write after close must fail")
	}
	s.Close() // idempotent
}

func TestConcurrentWriters(t *testing.T) {
	c := &capture{}
	s := NewSink(c.flush, 64, 10*time.Millisecond)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				s.Write(context.Background(), models.TelemetryEvent{NodeID: "n"})
			}
		}()
	}
	wg.Wait()
	s.Close()
	if _, total := c.snapshot(); total != 4000 {
		t.Fatalf("flushed %d of 4000", total)
	}
}

// Integration: runs against a real TimescaleDB (or plain Postgres) when TIMESCALE_DSN is set,
// for example postgres://nexus_admin:supersecretpassword@127.0.0.1:5434/nexus_telemetry
func TestAgainstRealDatabase(t *testing.T) {
	dsn := os.Getenv("TIMESCALE_DSN")
	if dsn == "" {
		t.Skip("TIMESCALE_DSN not set")
	}
	ctx := context.Background()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Truncate(ctx)
	t.Logf("hypertable active: %v", st.Hypertable)
	if os.Getenv("EXPECT_HYPERTABLE") == "1" && !st.Hypertable {
		t.Fatal("expected the timescaledb extension to be active")
	}

	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Minute)
	var evs []models.TelemetryEvent
	for i := 0; i < 600; i++ { // 10 minutes, one reading per second, 3 nodes
		evs = append(evs, models.TelemetryEvent{
			NodeID: fmt.Sprintf("n%d", i%3), NodeType: "PROSUMER", Timestamp: base.Add(time.Duration(i) * time.Second),
			CurrentConsumption: 1, ReplenishmentRate: 2, StateOfCharge: 50 + float64(i%10), LocalFrequency: 50 - float64(i%7)*0.1, Voltage: 230,
		})
	}
	if err := st.Insert(ctx, evs); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.Count(ctx); n != 600 {
		t.Fatalf("count = %d, want 600", n)
	}
	recent, err := st.Recent(ctx, "n1", 5)
	if err != nil || len(recent) != 5 || !recent[0].Timestamp.After(recent[1].Timestamp) {
		t.Fatalf("recent: %v err %v", recent, err)
	}
	buckets, err := st.Buckets(ctx, time.Minute, base)
	if err != nil || len(buckets) != 10 {
		t.Fatalf("expected 10 one-minute buckets, got %d (err %v)", len(buckets), err)
	}
	for _, b := range buckets {
		if b.Samples != 60 || b.MinFreq > b.AvgFreq {
			t.Fatalf("bad bucket %+v", b)
		}
	}
	if st.Hypertable {
		var chunks int
		if err := st.pool.QueryRow(ctx, `SELECT count(*) FROM timescaledb_information.chunks WHERE hypertable_name = 'telemetry'`).Scan(&chunks); err != nil || chunks < 1 {
			t.Fatalf("hypertable has no chunks: %d %v", chunks, err)
		}
	}
}
