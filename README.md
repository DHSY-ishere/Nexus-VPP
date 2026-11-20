#  Project Nexus: Autonomous Frequency Synchronization & Decentralized VPP

![Go](https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![Apache Kafka](https://img.shields.io/badge/Apache_Kafka-231F20?style=for-the-badge&logo=apache-kafka&logoColor=white)
![Python](https://img.shields.io/badge/Python-14354C?style=for-the-badge&logo=python&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-316192?style=for-the-badge&logo=postgresql&logoColor=white)
![React Three Fiber](https://img.shields.io/badge/React_Three_Fiber-000000?style=for-the-badge&logo=react&logoColor=white)

**Project Nexus** is a highly concurrent, software-defined Virtual Power Plant (VPP) architecture designed to solve the critical "loss of mechanical inertia" problem in modern, renewable-heavy electrical grids. 

By aggregating 10,000+ Distributed Energy Resources (DERs) such as residential batteries and solar inverters into a single decentralized mesh, Nexus executes proportional power injections in under 100 milliseconds to autonomously stabilize grid frequency and prevent cascading blackouts.

---

## 📖 Table of Contents
- [The Problem: Grid Inertia Loss](#-the-problem-grid-inertia-loss)
- [Key Features](#-key-features)
- [System Architecture](#-system-architecture)
- [Mathematical Core (DSCR)](#-mathematical-core-dscr)
- [Simulation Results](#-simulation-results)
- [Getting Started](#-getting-started)
- [Academic Context](#-academic-context)

---

## 🚨 The Problem: Grid Inertia Loss
Traditional power grids rely on the physical spinning mass of fossil-fuel turbines to act as a "shock absorber" during power fluctuations. As grids transition to Inverter-Based Resources (IBRs) like Solar and Wind—which have zero physical inertia—the grid becomes highly susceptible to rapid Rate-of-Change-of-Frequency (RoCoF) events. Without intervention, this leads to immediate, cascading blackouts.

**Nexus provides "Software-Defined Inertia."** It synchronizes thousands of small residential batteries to inject power simultaneously, artificially mimicking the stabilizing force of a massive power plant.

---

## ✨ Key Features

* **High-Concurrency Orchestration:** Utilizes Golang Goroutine worker pools to manage 10,000+ independent nodes concurrently with $O(1)$ command delivery latency.
* **Dynamic Storage Contribution Ratio (DSCR):** A custom load-balancing algorithm that prevents battery degradation by scaling power dispatch based on real-time solar replenishment rates.
* **Grid-aware P2P market:** A 5-minute uniform-price call auction per feeder zone, followed by a network gate that cuts any trade the cables cannot carry. Price discovery comes from the auction's clearing price.
* **Verified settlement on a validator blockchain:** Power cannot be routed to one buyer, so delivery is verified for the whole feeder against an independent feeder-head meter, and settlements are signed by validators that each re-run the rules (`backend/delivery`, `backend/chain`).
* **Dynamic stability tariff:** Frequency-response energy is paid $0.15/kW at the 49.8 Hz trigger, rising linearly to $0.85/kW at 49.0 Hz (`backend/tariff`, `ml/tariff.py`).
* **Trained solar forecaster:** A gradient-boosted model (scikit-learn) predicts the cloud factor 15 minutes ahead and the arbiter dispatches before a shortfall reaches the grid. Trained on synthetic cloud data; see `ml/models/metrics.json` after `python forecast.py train` for the held-out results.
* **OpenADR 3.0-style demand response:** A VTN publishes events and a VEN turns the active event into a market signal (not certified; see `backend/openadr`).
* **3D Digital Twin Command Center:** A React Three Fiber procedural city rendering 180,000+ polygons at 60FPS to visualize cascading blackouts, grid strain, and P2P energy trades in real-time.

---

## 🏗️ System Architecture

Nexus is built on a 5-layer, event-driven microservices stack:

1. **Edge Nodes:** Agents publish $V, f, SoC$ telemetry over **MQTT** (`nexus/telemetry/<node>`); the Go bridge validates each message and receives dispatch commands back on `nexus/commands/<node>`.
2. **Messaging Backbone:** Apache Kafka topics (`nexus.telemetry.raw`, `nexus.commands`) between the Go ingestion workers and the Python arbiter. Exactly-once semantics are **not** configured.
3. **Orchestration Engine:** A Go service keeps live fleet state in **Redis** (atomic Lua aggregates; the code uses cluster-safe hash tags and a UniversalClient, but only a single Redis server has been run). Grid operators signal demand response through an **OpenADR** VTN.
4. **Prediction Engine:** A Python service serving the trained forecaster over **gRPC** and **FastAPI** (REST).
5. **Data & Visualization:** **TimescaleDB** hypertable for telemetry history, PostgreSQL for the legacy ledger, and WebGL/Three.js for real-time observability.

---

## Implementation status

Everything above is implemented and tested, except where noted. What is **not** established:

* **Scale and latency targets are design targets, not measurements:** 10,000+ nodes, dispatch under 100 ms, 5,000 events/s ingestion, 60 FPS with 180,000+ polygons. Measurements are planned.
* **The forecaster is trained on synthetic data.** Its gain over persistence on held-out synthetic days shows the pipeline works; it is not evidence about real weather.
* **Validators run inside one process.** The consensus rules and signatures are real, but there is no network layer between separate organisations.
* **Demand response is a subset of OpenADR 3.0 and is not certified.**
* **Redis was tested as a single server**, not as a running cluster.

Run the optional services with `docker compose up -d` in `infra/` (Kafka, PostgreSQL, Redis, TimescaleDB, Mosquitto) and `python serve.py` in `ml/`; see `docs/ARCHITECTURE.md`.

---

## 🧮 Mathematical Core (DSCR)
Nexus abandons naive threshold-based dispatch. To ensure equitable battery utilization, dispatch commands are weighted by the **Dynamic Storage Contribution Ratio**:

```math
ContributionRatio_i(t) = \min\left(1.0, \frac{Consumption_{current\_i}}{Replenishment_{current\_i}}\right)
