// Package fleetstate holds the live state of the node fleet: the latest telemetry of
// every node and fleet-wide aggregates, kept current as events stream in.
//
// Redis is the production store. Aggregates are maintained incrementally inside Redis by
// Lua scripts, so reading the fleet summary is O(log N) and does not scan 10,000 nodes.
// Every key shares the hash tag {fleet}, so the multi-key scripts are valid on a Redis
// Cluster as well as on a single server. Memory is a drop-in in-process store with the
// same behaviour, used when Redis is not configured and as the reference in tests.
package fleetstate

import (
	"context"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
)

const (
	TriggerHz  = 49.8 // nodes below this frequency are counted as strained
	LowBattery = 20.0 // state of charge (%) below which a node is counted as low
)

// Summary is the fleet at a glance.
type Summary struct {
	Nodes                int     `json:"nodes"`
	AvgSoC               float64 `json:"avg_soc"`
	TotalConsumptionKW   float64 `json:"total_consumption_kw"`
	TotalReplenishmentKW float64 `json:"total_replenishment_kw"`
	MinFreqHz            float64 `json:"min_freq_hz"`
	MaxFreqHz            float64 `json:"max_freq_hz"`
	Strained             int     `json:"strained"`    // nodes reporting frequency below TriggerHz
	LowBatteryNodes      int     `json:"low_battery"` // nodes with state of charge below LowBattery
}

// Store is the fleet-state API.
type Store interface {
	// Update records a node's latest telemetry.
	Update(ctx context.Context, ev models.TelemetryEvent) error
	// Get returns a node's latest telemetry.
	Get(ctx context.Context, nodeID string) (models.TelemetryEvent, bool, error)
	// Summary returns fleet-wide aggregates.
	Summary(ctx context.Context) (Summary, error)
	// Sweep removes nodes not heard from within maxAge and returns how many were removed.
	Sweep(ctx context.Context, maxAge time.Duration) (int, error)
}

// Sink adapts a Store to the ingestion pipeline.
type Sink struct{ Store Store }

func (Sink) Name() string { return "fleetstate" }
func (s Sink) Write(ctx context.Context, ev models.TelemetryEvent) error {
	return s.Store.Update(ctx, ev)
}
