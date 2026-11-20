# Measured results (simulation only)

Machine: Intel Core Ultra 5 225H (14 threads), Windows 11, Go 1.26.1.
Everything here is a **simulation on a linearised (DC-style) feeder with synthetic meters**. No real grid data,
no Kafka/Postgres in these runs. Validators run in one process.

## Reproduce
```
cd backend
go test ./... -count=1 -v                                                    # 51 tests
go run ./cmd/export -out ../results -steps 288 -hour 0                       # honest day
go run ./cmd/export -out ../results/lie -steps 120 -hour 9 -fault lie -fault-from 48
go run ./cmd/export -out ../results/underdelivery -steps 120 -hour 9 -fault tamper -fault-from 48
go test ./engine ./settlement -run xxx -bench . -benchmem -benchtime 100x
```

## 1. Settlement blockchain and pool delivery check

### Honest full day (288 five-minute intervals, 4 zones x 10 homes, seed 42, 4 validators, quorum 3)
- 288 blocks, every block signed by 4 of 4 validators; full outside audit passes
- 3,770 trades, **all settled**; 1,152 zone-intervals, **0 false conservation alarms**
- Released $119.998 of $120.991 claimed (99.2%). The ~0.8% gap is consistent with meter noise: fill is clamped at 100%, so
  noise can only lower it (cause not separately isolated).
- Money is conserved (wallets plus pool never negative)

### Attack A: a seller's meter lies (delivers 30%, signed meter claims 100%), from interval 48 of 120
The old per-pair check passes this attack; both meters agree with the trade.
- Before the attack: 0 of 171 zone-intervals with trades failed conservation (no false alarms)
- During the attack: **260 of 284 zone-intervals (91.5%) failed conservation** against the independent head meter
- Released $31.231 of $37.475 claimed (83.3%)
- Trades: 104 settled, 1,319 partial, 1 disputed
- **24 of 284 attacked zone-intervals went undetected** (the lie was below the noise tolerance of about 0.08 kWh)

### Attack B: a seller honestly meters a 30% delivery (same run length, from interval 48)
- 0 conservation failures (correct: the meters are truthful and add up)
- Released $31.148 of $37.467 claimed (83.1%); the shortfall is shared across buyers and the seller is penalised

A lying meter and an honest shortfall end with almost the same release (83.3% vs 83.1%): lying gains the
seller essentially nothing over honestly under-delivering.

### Blockchain tests (all pass)
- Forged settlement from a malicious proposer plus one Byzantine validator is rejected (2 of 4 signatures, need 3)
- Six kinds of edit to a committed block are all detected by audit; a forgery with recomputed hash still fails re-execution
- One crashed validator: blocks still finalise (3 of 4). Two crashed: no finality, no settlement, no money moves
- Proposer rotates through all 4 validators; pruning keeps the retained chain verifiable

## 2. Compute cost (Go benchmarks, 100 iterations each, so indicative, not precise)
| Benchmark | Time/op |
|---|---|
| One interval, 40 homes, **blockchain on** (4 validators) | 9.5 ms |
| One interval, 40 homes, blockchain off | 2.5 ms |
| => consensus cost per block | about 7 ms |
| Full outside audit of 288 blocks (re-executes every settlement) | 583 ms (about 2 ms per block) |
| One interval, 16 zones x 10 homes, chain off | 9.1 ms (earlier run) |
| One interval, 16 zones x 50 homes (800 homes), chain off | 200.6 ms (earlier run) |
| Ed25519 sign / verify one meter reading | 27 us / 88 us |
| Merkle root, 10 / 100 / 1000 trades | 4.6 / 41 / 440 us |
| Merkle proof verify (1000 leaves) | 2.6 us |

Validators run in one process, so the per-block cost is the sum of four validators' independent re-execution.
In a real deployment they run in parallel on separate machines, and a network round trip would dominate.

## 3. Market (unchanged from earlier measurement, re-run: all tests pass)
- Learned network gate: ready after 5 intervals, sensitivity error about 0.5%, clears about 98% of the volume a
  perfect-model gate would, worst stress rise 0.00000
- Feeder stress never rose above the passive baseline in any zone or interval

## 4. Forecaster, tariff and infrastructure integrations (added later; all simulation)

### Trained 15-minute-ahead solar forecaster (ml/forecast.py, gradient-boosted regressor)
Trained on 300 simulated days, evaluated on 100 held-out days (13,600 daytime samples). The data is a
**synthetic** regime-switching cloud process, so this shows the pipeline works, not that it predicts real weather.

| Metric (solar output per kWp, 15 min ahead) | Model | Persistence | Clear sky |
|---|---|---|---|
| RMSE, all samples | 0.0995 | 0.1095 | 0.2301 |
| MAE, all samples | 0.0556 | 0.0546 | |
| RMSE, cloud-front cases (n=1,136) | 0.2833 | 0.3273 | |

RMSE skill over persistence: 9.1% overall, 13.5% on cloud-front cases. **MAE is slightly worse than persistence**:
the model reduces large errors, not typical ones.

### Dynamic stability tariff
$0.15/kW at 49.8 Hz rising linearly to $0.85/kW at 49.0 Hz. Go and Python agree on shared test vectors.
Live, through Kafka and the Python arbiter with the forecaster served over gRPC, credits paid per kW were
0.325 at 49.6 Hz, 0.675 at 49.2 Hz and 0.850 at 49.0 Hz, each matching the formula (a few trades at phase
boundaries carried the previous phase price while still in flight).

### Integrations against the real services (Docker: Redis 7, TimescaleDB pg15, Mosquitto 2, Kafka, PostgreSQL 15)
- MQTT to Redis and TimescaleDB: 200 simulated nodes at 1 msg/s for 10 s published 1,987 messages; the bridge accepted
  1,987, rejected 0, dropped 0; Redis showed 200 nodes; TimescaleDB held all 1,987 readings in an active hypertable.
  The 180 msg/s is the simulator publishing with synchronous QoS 1 per node, not a measured limit of the system.
- Go to Python over gRPC: forecast call returned the same values as the REST endpoint.
- OpenADR: unauthenticated request returned 401; an event published with the token reached the engine within seconds
  (demand-response level 3 and a $0.12 price cap), and deleting the event cleared it.
- Under a $0.12 OpenADR price signal, 1,281 simulated trades all cleared at or below $0.12.
- Legacy hash-chain ledger on a real PostgreSQL scratch database: 291 blocks, `valid: true`, no errors (after fixing
  a timestamp-precision bug that made verification fail on any real database).

### Tests
Go: 88 top-level tests pass, 0 fail, 1 skip (the real-TimescaleDB test, which passed when run with a database).
Python: 20 tests pass. Real-service variants run when REDIS_ADDR, TIMESCALE_DSN or MQTT_URL are set.

## NOT measured (do not quote)
- End-to-end latency, Kafka throughput, WebSocket throughput over a network
- README claims: 10,000+ nodes, <100 ms dispatch, 180,000 polygons at 60 FPS
- Multi-machine consensus latency or throughput (validators share one process)
- The 5,000 events/s ingestion target and the 60 FPS rendering target (no benchmark was run)
- Real-weather forecast accuracy (the forecaster has only seen synthetic data)
- A running Redis Cluster (only a single server was run)
- Anything on real hardware, real meters or a real network
- Detection performance with realistic baseline forecast errors (the simulation has none)

## Files
- `zones.csv` per zone per interval: price, volumes, stress, voltage, learned-gate stats, conserved, excess_kwh, tolerance_kwh, claimed_usd, confirmed_usd
- `trades.csv` every trade: gate result, status (SETTLED / PARTIAL / DISPUTED), fill, charged
- `intervals.csv` per interval: Merkle root, block height, block hash, signatures, proposer, active fault
- `lie/` and `underdelivery/` the two attack runs (same three files)
