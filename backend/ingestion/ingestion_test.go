package ingestion

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
)

type recorder struct {
	name string
	mu   sync.Mutex
	got  map[string]int
	fail bool
}

func (r *recorder) Name() string { return r.name }
func (r *recorder) Write(_ context.Context, ev models.TelemetryEvent) error {
	if r.fail {
		return errors.New("boom")
	}
	r.mu.Lock()
	r.got[ev.NodeID]++
	r.mu.Unlock()
	return nil
}
func (r *recorder) total() (n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.got {
		n += v
	}
	return
}

func feed(n int) <-chan models.TelemetryEvent {
	ch := make(chan models.TelemetryEvent, 256)
	go func() {
		for i := 0; i < n; i++ {
			ch <- models.TelemetryEvent{NodeID: fmt.Sprintf("n%d", i%50)}
		}
		close(ch)
	}()
	return ch
}

func TestEveryEventReachesEverySink(t *testing.T) {
	a, b := &recorder{name: "a", got: map[string]int{}}, &recorder{name: "b", got: map[string]int{}}
	p := startWorkers(feed(5000), 16, a, b)
	p.Wait()
	if p.Processed() != 5000 || a.total() != 5000 || b.total() != 5000 {
		t.Fatalf("processed=%d a=%d b=%d", p.Processed(), a.total(), b.total())
	}
}

func TestFailingSinkDoesNotStarveTheOthers(t *testing.T) {
	bad := &recorder{name: "bad", got: map[string]int{}, fail: true}
	good := &recorder{name: "good", got: map[string]int{}}
	p := startWorkers(feed(1000), 8, bad, good)
	p.Wait()
	if good.total() != 1000 {
		t.Fatalf("healthy sink got %d of 1000", good.total())
	}
	if p.Errors("bad") != 1000 || p.Errors("good") != 0 {
		t.Fatalf("error counts wrong: bad=%d good=%d", p.Errors("bad"), p.Errors("good"))
	}
}
