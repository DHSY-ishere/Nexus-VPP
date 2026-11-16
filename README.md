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
* **P2P Energy Market (Continuous Double Auction):** A PostgreSQL-backed transactive energy ledger that dynamically surges energy prices during grid strain, incentivizing prosumers to voluntarily stabilize the grid.
* **Predictive AI/ML Engine:** Python/PyTorch Temporal Fusion Transformers forecast cloud-cover and schedule proactive load shedding before a frequency crash occurs.
* **3D Digital Twin Command Center:** A React Three Fiber procedural city rendering 180,000+ polygons at 60FPS to visualize cascading blackouts, grid strain, and P2P energy trades in real-time.

---

## 🏗️ System Architecture

Nexus is built on a 5-layer, event-driven microservices stack:

1. **Edge Nodes:** Lightweight Go agents pushing real-time $V, f, SoC$ telemetry.
2. **Messaging Backbone:** Apache Kafka topics (`nexus.telemetry`, `nexus.commands`) configured with Exactly-Once Semantics (EOS) to guarantee zero duplicate dispatch commands during network partitions.
3. **Orchestration Engine:** Golang state controller managing fleet state via a Redis Cluster.
4. **Prediction Engine:** Python (FastAPI) microservice providing 15-minute ahead generation forecasts via gRPC.
5. **Data & Visualization:** TimescaleDB for telemetry, PostgreSQL for the P2P ledger, and WebGL/Three.js for real-time observability.

---

## 🧮 Mathematical Core (DSCR)
Nexus abandons naive threshold-based dispatch. To ensure equitable battery utilization, dispatch commands are weighted by the **Dynamic Storage Contribution Ratio**:

```math
ContributionRatio_i(t) = \min\left(1.0, \frac{Consumption_{current\_i}}{Replenishment_{current\_i}}\right)
