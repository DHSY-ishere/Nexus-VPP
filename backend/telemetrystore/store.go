// Package telemetrystore keeps telemetry history in TimescaleDB.
//
// Telemetry is an append-only time series, so it lives in a Timescale hypertable
// partitioned by time, with a retention policy and a one-minute continuous aggregate
// for dashboards. On plain PostgreSQL (no timescaledb extension) the store still works:
// it falls back to an ordinary table and date_bin, and Hypertable reports false.
//
// Writes are batched (see Sink) and sent with the COPY protocol.
package telemetrystore

import (
	"context"
	"fmt"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Retention is how long raw telemetry is kept when running on Timescale.
const Retention = "30 days"

// Store is a TimescaleDB-backed telemetry store.
type Store struct {
	pool       *pgxpool.Pool
	Hypertable bool // true when the timescaledb extension is active
}

// Open connects and creates the schema.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	s := &Store{pool: pool}
	if err := s.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() { s.pool.Close() }

// Migrate creates the table, then upgrades it to a hypertable when Timescale is available.
func (s *Store) Migrate(ctx context.Context) error {
	base := []string{
		`CREATE TABLE IF NOT EXISTS telemetry (
			time             timestamptz      NOT NULL,
			node_id          text             NOT NULL,
			node_type        text             NOT NULL,
			consumption_kw   double precision NOT NULL,
			replenishment_kw double precision NOT NULL,
			soc_percent      double precision NOT NULL,
			frequency_hz     double precision NOT NULL,
			voltage_v        double precision NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS telemetry_node_time ON telemetry (node_id, time DESC)`,
	}
	for _, q := range base {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// Timescale features. Each statement runs on its own: continuous aggregates cannot be
	// created inside a transaction block.
	if _, err := s.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS timescaledb`); err != nil {
		return nil // plain PostgreSQL: keep the ordinary table
	}
	if _, err := s.pool.Exec(ctx, `SELECT create_hypertable('telemetry', 'time', if_not_exists => TRUE)`); err != nil {
		return nil
	}
	s.Hypertable = true
	for _, q := range []string{
		`SELECT add_retention_policy('telemetry', INTERVAL '` + Retention + `', if_not_exists => TRUE)`,
		`CREATE MATERIALIZED VIEW IF NOT EXISTS telemetry_1m WITH (timescaledb.continuous) AS
			SELECT time_bucket('1 minute', time) AS bucket,
			       count(*)          AS samples,
			       avg(frequency_hz) AS avg_freq,
			       min(frequency_hz) AS min_freq,
			       avg(soc_percent)  AS avg_soc
			FROM telemetry GROUP BY bucket WITH NO DATA`,
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("timescale setup: %w", err)
		}
	}
	return nil
}

var columns = []string{"time", "node_id", "node_type", "consumption_kw", "replenishment_kw", "soc_percent", "frequency_hz", "voltage_v"}

// Insert writes a batch with COPY. Events without a timestamp are stamped with the current time.
func (s *Store) Insert(ctx context.Context, evs []models.TelemetryEvent) error {
	if len(evs) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make([][]any, len(evs))
	for i, e := range evs {
		ts := e.Timestamp
		if ts.IsZero() {
			ts = now
		}
		rows[i] = []any{ts, e.NodeID, e.NodeType, e.CurrentConsumption, e.ReplenishmentRate, e.StateOfCharge, e.LocalFrequency, e.Voltage}
	}
	_, err := s.pool.CopyFrom(ctx, pgx.Identifier{"telemetry"}, columns, pgx.CopyFromRows(rows))
	return err
}

// Recent returns a node's newest readings, newest first.
func (s *Store) Recent(ctx context.Context, nodeID string, limit int) ([]models.TelemetryEvent, error) {
	rows, err := s.pool.Query(ctx, `SELECT time, node_id, node_type, consumption_kw, replenishment_kw, soc_percent, frequency_hz, voltage_v
		FROM telemetry WHERE node_id = $1 ORDER BY time DESC LIMIT $2`, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.TelemetryEvent
	for rows.Next() {
		var e models.TelemetryEvent
		if err := rows.Scan(&e.Timestamp, &e.NodeID, &e.NodeType, &e.CurrentConsumption, &e.ReplenishmentRate, &e.StateOfCharge, &e.LocalFrequency, &e.Voltage); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Bucket is fleet telemetry summarised over one time bucket.
type Bucket struct {
	Start   time.Time `json:"start"`
	Samples int64     `json:"samples"`
	AvgFreq float64   `json:"avg_freq_hz"`
	MinFreq float64   `json:"min_freq_hz"`
	AvgSoC  float64   `json:"avg_soc"`
}

// Buckets summarises telemetry since a time in buckets of the given width, oldest first.
func (s *Store) Buckets(ctx context.Context, width time.Duration, since time.Time) ([]Bucket, error) {
	fn := `date_bin($1::interval, time, TIMESTAMPTZ '2000-01-01')`
	if s.Hypertable {
		fn = `time_bucket($1::interval, time)`
	}
	q := `SELECT ` + fn + ` AS bucket, count(*), avg(frequency_hz), min(frequency_hz), avg(soc_percent)
		FROM telemetry WHERE time >= $2 GROUP BY bucket ORDER BY bucket`
	rows, err := s.pool.Query(ctx, q, fmt.Sprintf("%d seconds", int(width.Seconds())), since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Start, &b.Samples, &b.AvgFreq, &b.MinFreq, &b.AvgSoC); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Count returns the number of stored readings.
func (s *Store) Count(ctx context.Context) (n int64, err error) {
	err = s.pool.QueryRow(ctx, `SELECT count(*) FROM telemetry`).Scan(&n)
	return
}

// Truncate empties the table (tests).
func (s *Store) Truncate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `TRUNCATE telemetry`)
	return err
}
