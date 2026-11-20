package fleetstate

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
	"github.com/redis/go-redis/v9"
)

// All keys carry the {fleet} hash tag so they map to one slot on a Redis Cluster.
const (
	kTotals = "nexus:{fleet}:totals" // hash: count, sum_soc, sum_cons, sum_repl
	kFreq   = "nexus:{fleet}:freq"   // zset: node -> frequency
	kSoC    = "nexus:{fleet}:soc"    // zset: node -> state of charge
	kSeen   = "nexus:{fleet}:seen"   // zset: node -> last-seen unix ms
)

func nodeKey(id string) string { return "nexus:{fleet}:node:" + id }

// updateScript keeps the totals exact by subtracting a node's previous values before
// adding its new ones. Running inside Redis makes the read-modify-write atomic.
var updateScript = redis.NewScript(`
local node, totals = KEYS[1], KEYS[2]
local soc, cons, repl = tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4])
local old = redis.call('HMGET', node, 'soc', 'cons', 'repl')
if old[1] == false then
  redis.call('HINCRBY', totals, 'count', 1)
  redis.call('HINCRBYFLOAT', totals, 'sum_soc', soc)
  redis.call('HINCRBYFLOAT', totals, 'sum_cons', cons)
  redis.call('HINCRBYFLOAT', totals, 'sum_repl', repl)
else
  redis.call('HINCRBYFLOAT', totals, 'sum_soc', soc - tonumber(old[1]))
  redis.call('HINCRBYFLOAT', totals, 'sum_cons', cons - tonumber(old[2]))
  redis.call('HINCRBYFLOAT', totals, 'sum_repl', repl - tonumber(old[3]))
end
redis.call('HSET', node, 'soc', ARGV[2], 'cons', ARGV[3], 'repl', ARGV[4], 'freq', ARGV[5],
  'volt', ARGV[6], 'type', ARGV[7], 'ts', ARGV[8])
redis.call('ZADD', KEYS[3], tonumber(ARGV[5]), ARGV[1])
redis.call('ZADD', KEYS[4], soc, ARGV[1])
redis.call('ZADD', KEYS[5], tonumber(ARGV[9]), ARGV[1])
return 1
`)

// sweepScript removes nodes last seen before ARGV[1] and takes them out of the totals.
// KEYS: totals, freq, seen, soc. ARGV: cutoff, node key prefix.
var sweepScript = redis.NewScript(`
local ids = redis.call('ZRANGEBYSCORE', KEYS[3], '-inf', ARGV[1])
for _, id in ipairs(ids) do
  local nk = ARGV[2] .. id
  local old = redis.call('HMGET', nk, 'soc', 'cons', 'repl')
  if old[1] ~= false then
    redis.call('HINCRBY', KEYS[1], 'count', -1)
    redis.call('HINCRBYFLOAT', KEYS[1], 'sum_soc', -tonumber(old[1]))
    redis.call('HINCRBYFLOAT', KEYS[1], 'sum_cons', -tonumber(old[2]))
    redis.call('HINCRBYFLOAT', KEYS[1], 'sum_repl', -tonumber(old[3]))
  end
  redis.call('DEL', nk)
  redis.call('ZREM', KEYS[2], id)
  redis.call('ZREM', KEYS[4], id)
  redis.call('ZREM', KEYS[3], id)
end
return #ids
`)

// Redis is a Store backed by Redis (single server or cluster).
type Redis struct {
	c   redis.UniversalClient
	Now func() time.Time
}

// NewRedis connects through a go-redis UniversalClient: pass one address for a single
// server or several for a cluster.
func NewRedis(addrs []string, password string) *Redis {
	return &Redis{c: redis.NewUniversalClient(&redis.UniversalOptions{Addrs: addrs, Password: password}), Now: time.Now}
}

// NewRedisWithClient wraps an existing client.
func NewRedisWithClient(c redis.UniversalClient) *Redis { return &Redis{c: c, Now: time.Now} }

func (r *Redis) Ping(ctx context.Context) error { return r.c.Ping(ctx).Err() }
func (r *Redis) Close() error                   { return r.c.Close() }

func ftoa(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

func (r *Redis) Update(ctx context.Context, ev models.TelemetryEvent) error {
	return updateScript.Run(ctx, r.c,
		[]string{nodeKey(ev.NodeID), kTotals, kFreq, kSoC, kSeen},
		ev.NodeID, ftoa(ev.StateOfCharge), ftoa(ev.CurrentConsumption), ftoa(ev.ReplenishmentRate),
		ftoa(ev.LocalFrequency), ftoa(ev.Voltage), ev.NodeType, ev.Timestamp.Format(time.RFC3339Nano),
		r.Now().UnixMilli(),
	).Err()
}

func (r *Redis) Get(ctx context.Context, id string) (models.TelemetryEvent, bool, error) {
	m, err := r.c.HGetAll(ctx, nodeKey(id)).Result()
	if err != nil {
		return models.TelemetryEvent{}, false, err
	}
	if len(m) == 0 {
		return models.TelemetryEvent{}, false, nil
	}
	f := func(k string) float64 { v, _ := strconv.ParseFloat(m[k], 64); return v }
	ts, _ := time.Parse(time.RFC3339Nano, m["ts"])
	return models.TelemetryEvent{
		NodeID: id, NodeType: m["type"], Timestamp: ts, StateOfCharge: f("soc"),
		CurrentConsumption: f("cons"), ReplenishmentRate: f("repl"), LocalFrequency: f("freq"), Voltage: f("volt"),
	}, true, nil
}

func (r *Redis) Summary(ctx context.Context) (Summary, error) {
	pipe := r.c.Pipeline()
	totals := pipe.HGetAll(ctx, kTotals)
	minF := pipe.ZRangeWithScores(ctx, kFreq, 0, 0)
	maxF := pipe.ZRevRangeWithScores(ctx, kFreq, 0, 0)
	strained := pipe.ZCount(ctx, kFreq, "-inf", fmt.Sprintf("(%s", ftoa(TriggerHz)))
	low := pipe.ZCount(ctx, kSoC, "-inf", fmt.Sprintf("(%s", ftoa(LowBattery)))
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return Summary{}, err
	}
	t := totals.Val()
	n, _ := strconv.Atoi(t["count"])
	if n <= 0 {
		return Summary{}, nil
	}
	f := func(k string) float64 { v, _ := strconv.ParseFloat(t[k], 64); return v }
	s := Summary{
		Nodes: n, AvgSoC: f("sum_soc") / float64(n),
		TotalConsumptionKW: f("sum_cons"), TotalReplenishmentKW: f("sum_repl"),
		Strained: int(strained.Val()), LowBatteryNodes: int(low.Val()),
	}
	if v := minF.Val(); len(v) > 0 {
		s.MinFreqHz = v[0].Score
	}
	if v := maxF.Val(); len(v) > 0 {
		s.MaxFreqHz = v[0].Score
	}
	return s, nil
}

func (r *Redis) Sweep(ctx context.Context, maxAge time.Duration) (int, error) {
	cutoff := r.Now().Add(-maxAge).UnixMilli()
	return sweepScript.Run(ctx, r.c,
		[]string{kTotals, kFreq, kSeen, kSoC}, cutoff, "nexus:{fleet}:node:").Int()
}
