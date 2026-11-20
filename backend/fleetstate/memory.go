package fleetstate

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
)

type memEntry struct {
	ev   models.TelemetryEvent
	seen time.Time
}

// Memory is an in-process Store.
type Memory struct {
	mu    sync.RWMutex
	nodes map[string]memEntry
	Now   func() time.Time
}

func NewMemory() *Memory { return &Memory{nodes: map[string]memEntry{}, Now: time.Now} }

func (m *Memory) Update(_ context.Context, ev models.TelemetryEvent) error {
	m.mu.Lock()
	m.nodes[ev.NodeID] = memEntry{ev, m.Now()}
	m.mu.Unlock()
	return nil
}

func (m *Memory) Get(_ context.Context, id string) (models.TelemetryEvent, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.nodes[id]
	return e.ev, ok, nil
}

func (m *Memory) Summary(_ context.Context) (Summary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := Summary{MinFreqHz: math.Inf(1), MaxFreqHz: math.Inf(-1)}
	var soc float64
	for _, e := range m.nodes {
		s.Nodes++
		soc += e.ev.StateOfCharge
		s.TotalConsumptionKW += e.ev.CurrentConsumption
		s.TotalReplenishmentKW += e.ev.ReplenishmentRate
		s.MinFreqHz = math.Min(s.MinFreqHz, e.ev.LocalFrequency)
		s.MaxFreqHz = math.Max(s.MaxFreqHz, e.ev.LocalFrequency)
		if e.ev.LocalFrequency < TriggerHz {
			s.Strained++
		}
		if e.ev.StateOfCharge < LowBattery {
			s.LowBatteryNodes++
		}
	}
	if s.Nodes == 0 {
		return Summary{}, nil
	}
	s.AvgSoC = soc / float64(s.Nodes)
	return s, nil
}

func (m *Memory) Sweep(_ context.Context, maxAge time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff, n := m.Now().Add(-maxAge), 0
	for id, e := range m.nodes {
		if e.seen.Before(cutoff) {
			delete(m.nodes, id)
			n++
		}
	}
	return n, nil
}
