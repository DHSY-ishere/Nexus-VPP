package fleetstate

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// clock lets tests move time without sleeping.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type impl struct {
	name  string
	store func(t *testing.T, clk *clock) Store
}

func impls() []impl {
	out := []impl{
		{"memory", func(t *testing.T, clk *clock) Store { m := NewMemory(); m.Now = clk.Now; return m }},
		{"miniredis", func(t *testing.T, clk *clock) Store {
			s := miniredis.RunT(t)
			r := NewRedisWithClient(redis.NewClient(&redis.Options{Addr: s.Addr()}))
			r.Now = clk.Now
			t.Cleanup(func() { r.Close() })
			return r
		}},
	}
	if addr := os.Getenv("REDIS_ADDR"); addr != "" { // a real server (docker compose up redis)
		out = append(out, impl{"real-redis", func(t *testing.T, clk *clock) Store {
			c := redis.NewClient(&redis.Options{Addr: addr})
			c.FlushDB(context.Background())
			r := NewRedisWithClient(c)
			r.Now = clk.Now
			t.Cleanup(func() { c.FlushDB(context.Background()); r.Close() })
			return r
		}})
	}
	return out
}

func ev(id string, soc, cons, repl, freq float64) models.TelemetryEvent {
	return models.TelemetryEvent{NodeID: id, NodeType: "PROSUMER", Timestamp: time.Unix(1_700_000_000, 0).UTC(),
		StateOfCharge: soc, CurrentConsumption: cons, ReplenishmentRate: repl, LocalFrequency: freq, Voltage: 230}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func sameSummary(a, b Summary) bool {
	return a.Nodes == b.Nodes && near(a.AvgSoC, b.AvgSoC) && near(a.TotalConsumptionKW, b.TotalConsumptionKW) &&
		near(a.TotalReplenishmentKW, b.TotalReplenishmentKW) && near(a.MinFreqHz, b.MinFreqHz) &&
		near(a.MaxFreqHz, b.MaxFreqHz) && a.Strained == b.Strained && a.LowBatteryNodes == b.LowBatteryNodes
}

func TestStoreBehaviour(t *testing.T) {
	ctx := context.Background()
	for _, im := range impls() {
		t.Run(im.name, func(t *testing.T) {
			clk := &clock{t: time.Unix(1_700_000_000, 0)}
			s := im.store(t, clk)

			if sum, err := s.Summary(ctx); err != nil || sum.Nodes != 0 {
				t.Fatalf("empty fleet: %+v %v", sum, err)
			}
			s.Update(ctx, ev("a", 80, 1.0, 3.0, 50.0))
			s.Update(ctx, ev("b", 10, 2.0, 0.0, 49.5)) // strained and low battery
			s.Update(ctx, ev("c", 60, 0.5, 2.0, 50.1))
			sum, _ := s.Summary(ctx)
			want := Summary{Nodes: 3, AvgSoC: 50, TotalConsumptionKW: 3.5, TotalReplenishmentKW: 5,
				MinFreqHz: 49.5, MaxFreqHz: 50.1, Strained: 1, LowBatteryNodes: 1}
			if !sameSummary(sum, want) {
				t.Fatalf("summary\n got  %+v\n want %+v", sum, want)
			}

			// Updating a node replaces its values; it is not counted twice.
			s.Update(ctx, ev("b", 90, 0.5, 4.0, 50.0))
			sum, _ = s.Summary(ctx)
			want = Summary{Nodes: 3, AvgSoC: 76.666666667, TotalConsumptionKW: 2.0, TotalReplenishmentKW: 9,
				MinFreqHz: 50.0, MaxFreqHz: 50.1}
			if !sameSummary(sum, want) {
				t.Fatalf("after update\n got  %+v\n want %+v", sum, want)
			}

			got, ok, err := s.Get(ctx, "b")
			if err != nil || !ok || got.StateOfCharge != 90 || got.ReplenishmentRate != 4.0 || got.NodeType != "PROSUMER" {
				t.Fatalf("Get b = %+v ok=%v err=%v", got, ok, err)
			}
			if _, ok, _ := s.Get(ctx, "nope"); ok {
				t.Fatal("unknown node found")
			}
		})
	}
}

func TestSweepExpiresSilentNodes(t *testing.T) {
	ctx := context.Background()
	for _, im := range impls() {
		t.Run(im.name, func(t *testing.T) {
			clk := &clock{t: time.Unix(1_700_000_000, 0)}
			s := im.store(t, clk)
			s.Update(ctx, ev("old1", 50, 1, 1, 49.0))
			s.Update(ctx, ev("old2", 50, 1, 1, 49.0))
			clk.Add(2 * time.Minute)
			s.Update(ctx, ev("fresh", 70, 2, 2, 50.0))

			n, err := s.Sweep(ctx, time.Minute)
			if err != nil || n != 2 {
				t.Fatalf("swept %d (err %v), want 2", n, err)
			}
			sum, _ := s.Summary(ctx)
			want := Summary{Nodes: 1, AvgSoC: 70, TotalConsumptionKW: 2, TotalReplenishmentKW: 2, MinFreqHz: 50, MaxFreqHz: 50}
			if !sameSummary(sum, want) {
				t.Fatalf("after sweep\n got  %+v\n want %+v", sum, want)
			}
			if _, ok, _ := s.Get(ctx, "old1"); ok {
				t.Fatal("expired node still present")
			}
			if n, _ := s.Sweep(ctx, time.Minute); n != 0 {
				t.Fatalf("second sweep removed %d", n)
			}
		})
	}
}

// Random updates and sweeps must leave Redis (with incremental Lua aggregates) agreeing with
// the simple in-memory reference at every step.
func TestRedisAggregatesMatchReferenceUnderRandomLoad(t *testing.T) {
	ctx := context.Background()
	for _, im := range impls() {
		if im.name == "memory" {
			continue
		}
		t.Run(im.name, func(t *testing.T) {
			clk := &clock{t: time.Unix(1_700_000_000, 0)}
			ref, s := NewMemory(), im.store(t, clk)
			ref.Now = clk.Now
			rng := rand.New(rand.NewSource(1))
			for i := 0; i < 3000; i++ {
				e := ev(fmt.Sprintf("n%d", rng.Intn(150)), rng.Float64()*100, rng.Float64()*5, rng.Float64()*6, 49+rng.Float64()*1.5)
				ref.Update(ctx, e)
				s.Update(ctx, e)
				clk.Add(time.Duration(rng.Intn(400)) * time.Millisecond)
				if i%250 == 249 {
					ref.Sweep(ctx, 20*time.Second)
					s.Sweep(ctx, 20*time.Second)
				}
				if i%50 == 0 {
					a, _ := ref.Summary(ctx)
					b, _ := s.Summary(ctx)
					if !sameSummary(a, b) {
						t.Fatalf("step %d diverged\n ref   %+v\n store %+v", i, a, b)
					}
				}
			}
		})
	}
}

func TestConcurrentUpdates(t *testing.T) {
	ctx := context.Background()
	for _, im := range impls() {
		t.Run(im.name, func(t *testing.T) {
			s := im.store(t, &clock{t: time.Unix(1_700_000_000, 0)})
			var wg sync.WaitGroup
			for w := 0; w < 8; w++ {
				wg.Add(1)
				go func(w int) {
					defer wg.Done()
					for i := 0; i < 100; i++ {
						s.Update(ctx, ev(fmt.Sprintf("w%d-n%d", w, i%25), 50, 1, 1, 50))
					}
				}(w)
			}
			wg.Wait()
			if sum, _ := s.Summary(ctx); sum.Nodes != 200 || !near(sum.TotalConsumptionKW, 200) {
				t.Fatalf("expected 200 nodes totalling 200 kW, got %+v", sum)
			}
		})
	}
}

// crc16 (XMODEM) as Redis Cluster uses it to map keys to hash slots.
func crc16(b []byte) uint16 {
	var crc uint16
	for _, c := range b {
		crc ^= uint16(c) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// slot applies Redis Cluster's hash-tag rule: if a key contains {tag}, only the tag is hashed.
func slot(key string) uint16 {
	if i := strings.IndexByte(key, '{'); i >= 0 {
		if j := strings.IndexByte(key[i+1:], '}'); j > 0 {
			key = key[i+1 : i+1+j]
		}
	}
	return crc16([]byte(key)) % 16384
}

// The Lua scripts touch several keys at once, which a Redis Cluster only allows when every key
// lives in the same hash slot. The {fleet} tag guarantees that, for any node id.
func TestAllKeysShareOneClusterSlot(t *testing.T) {
	if crc16([]byte("123456789")) != 0x31C3 { // the standard CRC-16/XMODEM check value
		t.Fatal("crc16 implementation is wrong")
	}
	want := slot(kTotals)
	for _, k := range []string{kFreq, kSoC, kSeen, nodeKey("home-0001"), nodeKey("node_4999"), nodeKey("x}y"), nodeKey("{evil}")} {
		if slot(k) != want {
			t.Fatalf("key %q is in slot %d, the script keys are in %d: a cluster would reject the script", k, slot(k), want)
		}
	}
}
