# Nexus VPP: layered architecture, as implemented

This maps the seven-layer proposal to the code. Layers depend only on the layer below.

| Layer | What it does | Code | Status |
|---|---|---|---|
| L1 Physical assets | Radial low-voltage feeder: cables with thermal ratings and resistance, voltage limits | `backend/feeder` | Simulated (parameters are plausible, not from a real network) |
| L2 Edge agent | One per home: owner limits, battery wear floor, bid curves, reflex response | `backend/agent` | Implemented |
| L3 Messaging | Kafka telemetry (existing); market runs in-process over WebSocket | `backend/ingestion`, `main.go` | Kafka path unchanged. No MQTT, OpenADR or mesh fallback |
| L4 Grid-aware market | 5-minute uniform-price call auction per zone, then a network gate | `backend/market` | Implemented (see "What the market trades") |
| L5 Settlement | Ed25519 signed meter readings, escrow, delivery check, Merkle root per interval | `backend/settlement` | Implemented; legacy `MineBlock` chain still serves the old P2P page |
| L6 Twin | Counterfactual (Shapley) attribution; learned-sensitivity network gate | `backend/twin`, `backend/engine` | Both live. The market gate runs on a network learned from home probes (see below) |
| L7 Experience | Live market page: zone prices, feeder health, gate decisions, proof checking | `frontend/app/(platform)/market` | Implemented. Prosumer, DSO and audit views are not built |

Orchestration: `backend/engine` runs one interval at a time (bids, clearing, gate, dispatch, meters, escrow, attribution, Merkle root).

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
- Merkle roots are stored in Postgres and served over HTTP. They are not anchored to any external system, so on their own they prove consistency, not independence from the operator.
- `engine` keeps the last two hours of proofs in memory.
- Patent claims need a professional prior-art search; nothing here is legal advice.
