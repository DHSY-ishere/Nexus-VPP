# Nexus VPP: layered architecture, as implemented

This maps the seven-layer proposal to the code. Layers depend only on the layer below.

| Layer | What it does | Code | Status |
|---|---|---|---|
| L1 Physical assets | Radial low-voltage feeder: cables with thermal ratings and resistance, voltage limits | `backend/feeder` | Simulated (parameters are plausible, not from a real network) |
| L2 Edge agent | One per home: owner limits, battery wear floor, bid curves, reflex response | `backend/agent` | Implemented |
| L3 Messaging | MQTT from the edge, Kafka between the ingestion workers and the arbiter, OpenADR 3.0-style demand-response signals; the market itself runs in-process over WebSocket | `backend/mqttbridge`, `backend/ingestion`, `backend/openadr`, `main.go` | Implemented. No mesh fallback. OpenADR is a subset and not certified |
| L4 Grid-aware market | 5-minute uniform-price call auction per zone, then a network gate | `backend/market` | Implemented (see "What the market trades") |
| L5 Settlement | Signed meters for every home plus an independent feeder-head meter, pool delivery check, escrow, validator-signed blockchain with a Merkle root per interval | `backend/delivery`, `backend/chain`, `backend/settlement` | Implemented (validators run in one process; see "Settlement blockchain" below). The legacy `MineBlock` chain still serves the old P2P page |
| L6 Twin | Counterfactual (Shapley) attribution; learned-sensitivity network gate | `backend/twin`, `backend/engine` | Both live. The market gate runs on a network learned from home probes (see below) |
| L7 Experience | Live market page: zone prices, feeder health, gate decisions, proof checking | `frontend/app/(platform)/market` | Implemented. Prosumer, DSO and audit views are not built |

Orchestration: `backend/engine` runs one interval at a time (bids, clearing, gate, dispatch, meters, escrow, attribution, Merkle root).

## Settlement blockchain and the delivery problem

**The problem.** Electricity is fungible. When a seller injects and a buyer draws on the same feeder, no
measurement can show that the buyer's electrons came from that seller, so "did seller A deliver to buyer B" has
nothing physical to check. A pair-wise check (each side's meter against the trade) also passes when a seller's
own meter simply lies.

**What `backend/delivery` does instead** (`delivery.Settle`, a pure function):

1. **Additionality.** Every home signs a net-energy reading. A seller delivered if its export rose above its
   baseline by the traded amount; a buyer received if its draw rose above its baseline.
2. **Conservation anchor.** A utility-owned feeder-head meter signs the zone's net flow. Homes' readings must equal
   head reading plus cable losses (I squared R from the public feeder model), within 5 standard deviations of
   meter noise. A seller whose meter claims delivery that never happened breaks this equation, and no single
   meter can hide it.
3. **Pool settlement.** Delivery is settled on the zone as a pool, not per pair. Matched value =
   min(seller delivery confirmed by their own meters, buyer absorption confirmed by theirs), cut by any amount the
   head meter cannot confirm. Sellers are paid in proportion to their own measured delivery; buyers are charged in
   proportion to their own measured draw. A shortfall is therefore shared by all buyers pro rata, because it cannot
   be pinned on a specific buyer. A seller short by more than meter tolerance pays 25% of the shortfall value to
   the buyers who went unfilled. With no valid head reading nothing is confirmed and all escrow is refunded.

**What `backend/chain` adds.** A permissioned blockchain. One block per interval holds the raw evidence (signed
meter readings, head reading, cleared trades) and the settlement outcome. Four validators rotate as proposer. A
validator signs only after it has re-run `delivery.Settle` itself and got an identical outcome and the same trade
Merkle root. A block is final with 3 of 4 signatures (2f+1, f=1). A block with a forged outcome cannot be finalised
by one Byzantine validator; one crashed validator does not stop finality. `Chain.Verify` re-audits every block
(hash links, proposer rotation, signature quorum, full re-execution), so any edit to history fails, even if the
attacker recomputes the block hash. In the engine, money moves only after a block is committed.

**API.** `/ledger/info`, `/ledger/verify`, `/ledger/block?height=N`, `/ledger/delivery?trade_id=ID` (a buyer's proof
of receipt: its signed reading, the seller's, the zone totals against the head meter, the block and its signatures).

### What this does and does not prove

- Validators are real signers running the real checks, but **all run inside one process**. There is no network
  layer, no peer discovery, and no separate organisations holding keys. It demonstrates the protocol and its
  failure handling, not a deployed multi-party network.
- The evidence is only as good as the meters. Meter keys are assumed to be genuine and tamper-resistant. A forged
  head meter is rejected; a compromised head meter is not detected.
- **Detection has a floor.** A lie smaller than the noise tolerance (about 0.08 kWh per zone-interval for ten
  homes) cannot be told apart from noise. A seller can steal at most about that much per zone-interval, roughly
  one cent at the simulated price. Measured: 24 of 284 attacked zone-intervals went undetected.
- **One equation, many suspects.** Conservation shows that a zone is inconsistent, not which meter lied. The pool
  settlement limits the damage, but the liar is not identified in a single interval (a seller whose meter claims
  full delivery is not flagged as in breach). Identifying a repeat offender across intervals is not implemented.
- **Baselines are taken as given.** Additionality is measured against each home's forecast baseline, and the
  simulation has no baseline forecast error. Real baselines are noisy, which would widen the tolerance and the
  floor above. Baselines are not committed before the interval, so a home could in principle declare a favourable
  one. Pooling and the head meter do not depend on baselines, so aggregate conservation still holds.
- **The auction is not verified on chain.** Bids are private and not recorded, so validators confirm settlement of
  the trades as cleared, not that the clearing itself was correct.
- Everything is simulated on a linearised feeder with synthetic meters.

## Infrastructure integrations

Each is optional and switched on by an environment variable (`backend/integrations`); anything not configured stays
off. `GET /integrations/status` reports what is running.

| Piece | What it does | Switch | Code |
|---|---|---|---|
| Redis | Live fleet state: each node's latest telemetry plus fleet aggregates kept current by atomic Lua scripts, so `GET /fleet/summary` is O(log N). All keys share the `{fleet}` hash tag so the scripts are valid on a Redis Cluster. Falls back to an in-process store | `REDIS_ADDR` | `backend/fleetstate` |
| TimescaleDB | Telemetry history in a hypertable with a 30-day retention policy and a one-minute continuous aggregate; batched COPY writes. `GET /telemetry/recent`, `/telemetry/buckets`. Falls back to a plain table on ordinary PostgreSQL | `TIMESCALE_DSN` | `backend/telemetrystore` |
| MQTT | Edge nodes publish telemetry to `nexus/telemetry/<node>`; the bridge validates each message (a node cannot speak for another, values must be plausible) and feeds ingestion; commands go down on `nexus/commands/<node>` | `MQTT_BROKER` | `backend/mqttbridge`, `cmd/edgesim` |
| gRPC + FastAPI | The Python forecast service: gRPC on :50051 (contract in `proto/forecast.proto`, Go client in `backend/forecastclient`) and REST on :8000. `GET /forecast` on the Go server proxies the gRPC call | `FORECAST_GRPC` | `ml/serve.py` |
| OpenADR | A VTN (programs and events, bearer-token access) and a VEN that polls it. The active event becomes a demand-response signal: level 0-3 lowers every home's target state of charge (homes buy less, offer more) and a PRICE payload caps buyers' bids | `OPENADR=1`, `OPENADR_TOKEN` | `backend/openadr` |
| Dynamic tariff | Frequency-response energy is paid $0.15/kW at 49.8 Hz, rising linearly to $0.85/kW at 49.0 Hz. Go and Python share test vectors | always on | `backend/tariff`, `ml/tariff.py` |
| Trained forecaster | Gradient-boosted regressor predicting the cloud factor 15 minutes ahead. The arbiter dispatches either reactively (frequency below 49.8 Hz) or before a forecast solar shortfall reaches the grid | `FORECAST_MODE=local\|grpc` | `ml/forecast.py`, `ml/processor.py` |

Run: `docker compose up -d` in `infra/`, `python serve.py` in `ml/`, then for example

```
REDIS_ADDR=127.0.0.1:6379 TIMESCALE_DSN=postgres://nexus_admin:supersecretpassword@127.0.0.1:5434/nexus_telemetry \
MQTT_BROKER=tcp://127.0.0.1:1883 FORECAST_GRPC=127.0.0.1:50051 OPENADR=1 OPENADR_TOKEN=change-me \
  go run ./cmd/marketdemo            # market only, telemetry arrives over MQTT
go run ./cmd/edgesim -nodes 200      # simulated edge nodes publishing over MQTT
```

### What this does and does not establish

- **The forecaster is trained on synthetic data** (a regime-switching cloud process with afternoon build-up). Its gain
  over persistence on held-out synthetic days shows the training and serving pipeline works. It says nothing about
  real weather. MAE is not better than persistence (slightly worse); RMSE is better, mainly on cloud-front cases.
- **Redis was run as a single server.** The code is cluster-safe by construction (a test checks every key maps to one
  slot) but a running cluster was not tested. Aggregates are floating-point sums updated incrementally, so tiny
  rounding drift accumulates over millions of updates; a periodic recompute would remove it.
- **OpenADR is a subset** (programs and events, SIMPLE / PRICE / DISPATCH_SETPOINT payloads). No reports, webhooks,
  VEN registration or OAuth issuance. Not certified. DISPATCH_SETPOINT is parsed but not acted on.
- **MQTT** is exercised against Mosquitto without TLS or authentication (anonymous access, local development only).
  Production needs TLS and per-node credentials or certificates; topic-level access control is what would really
  stop one node publishing as another, because the bridge's node-id check relies on a node not owning another's topic.
- **The legacy chain** (`MineBlock`) hashed its timestamp at nanosecond precision in the local zone, which PostgreSQL
  cannot reproduce, so `/chain/verify` failed on a real database. Fixed for new blocks by hashing UTC at microsecond
  precision. Blocks mined before the fix cannot be verified (their nanoseconds were never stored).
- Kafka topics were not created automatically, so the first run lost telemetry. The writer now creates the topic.
- Not measured: ingestion throughput (the 5,000 events/s target), rendering frame rate, and any multi-machine latency.

## What the market trades

Passive net positions (solar minus load) flow through the feeder whatever the market does, so a purely financial trade would not change physics. The market therefore clears **flexibility increments**: a seller injects extra energy (battery discharge), a buyer absorbs extra energy (battery or EV charging). The network gate checks baseline plus increments against cable ratings and the 0.94 to 1.06 pu voltage band, and cuts any pairing that would raise feeder stress. Buyers are matched with the electrically nearest in-the-money seller.

## The learned network gate

The gate does not need a cable-impedance model. Each interval, two homes per zone take turns stepping their battery power by a fraction of a kW and record how their own voltage responds (8 readings each, 30 mV meter noise). On a radial feeder the sensitivity between any two buses equals the self-sensitivity of the bus nearer the transformer, so those self-sensitivities alone rebuild every bus voltage (`twin.LearnedNetwork`). Cable thermal ratings are taken as known nameplate data.

- **Warm-up:** until every bus has 6 or more readings (5 intervals, about 25 simulated minutes), the gate checks thermal limits only and the UI says so. Voltage problems in that window can be missed.
- **Accuracy:** in the full-day test the learned sensitivities are within about 0.5% of the truth and the gate clears 98% of the volume a perfect-model gate would, with no increase in true feeder stress.
- **Comparison:** every interval also clears with the true model (`model_cleared_kwh`) so the cost of not knowing the network is measured, not assumed. Set `Config.Gate = "model"` to run on the true model instead.
- **Not covered:** the learner accumulates and does not forget, so it would not follow a network that changes (tap changes, new cables). Probes also assume homes probe one at a time; simultaneous probing would need a different estimator.

## Running it

Full stack (needs Docker for Kafka and Postgres): `docker compose up -d` in `infra/`, then `go run .` in `backend/`, `npm run dev` in `frontend/`. Open `/market`.

Market only, no Docker: `go run ./cmd/marketdemo` in `backend/` (serves `/ws`, `/market/state`, `/market/proof` on :8080), then `npm run dev` in `frontend/`. Do not run both backends at once, they share port 8080.

Tests: `go test ./...` in `backend/`. They cover the auction, the gate (never raises feeder stress), settlement (signatures, escrow, Merkle proofs), attribution (Shapley efficiency), the learned gate against the model, and a full simulated day (every trade settles with honest meters; tampered meters get disputed; calibration stays realistic; the learned gate warms up fast, stays accurate, and never worsens the true feeder).

## Known limits

- The feeder model is linearised (DC-style). It is accurate for small voltage deviations only.
- Frequency is not modelled network-wide. Island mode applies priority and a price cap; it does not model islanded frequency dynamics.
- The learned-gate comparison is against a simulated feeder that follows the same linear model the learner assumes, so it shows the method works under that assumption, not on a real network.
- Reflex contracts are executed and paid, but not gated or verified from meter data.
- The solidarity pool accumulates a 2% levy; payouts to households are not implemented. Virtual rooftop shares are not implemented.
- Merkle roots and blocks are not anchored to any external system, and validators share one process, so on their own they prove consistency, not independence from the operator.
- `engine` keeps the last two hours of proofs in memory.
- Patent claims need a professional prior-art search; nothing here is legal advice.
