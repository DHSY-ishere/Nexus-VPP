# Nexus VPP (Virtual Power Plant): Complete System & Codebase Summary

This document provides a comprehensive summary of the **Nexus VPP** codebase, its architecture, file structures, technologies, and runtime features. You can provide this file to any LLM/AI to immediately give it context on how the system is put together.

---

## 1. Project Overview & Architecture
**Nexus VPP** is an end-to-end simulation environment representing an autonomous, decentralized **Virtual Power Plant (VPP)** and energy orchestration grid. The system is designed to simulate a high-throughput smart grid where "prosumers" (nodes that both consume and produce energy) publish real-time telemetry, settle energy transactions via a peer-to-peer (P2P) trading market, and stabilize grid frequency anomalies under simulated sabotage or weather events.

### System Architecture Flow:
```
  [Telemetry Events] ---> [Go Orchestrator (WebSocket Hub)] ---> [Kafka Telemetry Raw Topic]
                                     |
                                     v
                        [Python ML Engine Processor] (Detects strain)
                                     | (If frequency drops & SoC is sufficient)
                                     v
                        [Kafka Commands Topic] ---> [Go Orchestrator] ---> [Websocket Broadcast to UI]
                                                                        ---> [Postgres DB Ledger Commit]
```

---

## 2. Directory Structure & Key Files

```
nexus-vpp/
├── backend/                   # Go orchestrator & ingestion service
│   ├── main.go                # Central WebSocket hub, routing, and simulation runner
│   ├── db/
│   │   └── postgres.go        # PostgreSQL connection and GORM auto-migrations
│   ├── ingestion/
│   │   └── kafka.go           # High-throughput asynchronous worker pool for Kafka ingestion
│   └── models/
│       ├── ledger.go          # Database schema for P2P energy trades
│       └── telemetry.go       # Struct schema for real-time node telemetry
├── ml/                        # Python-based ML and grid arbitration logic
│   └── processor.py           # Confluent Kafka consumer/producer evaluating P2P trades
├── frontend/                  # Next.js React client with 3D and charting dashboards
│   ├── app/
│   │   ├── page.tsx           # Immersive landing page with video background
│   │   ├── globals.css        # Main stylesheet defining glassmorphism & CRT scanline classes
│   │   └── (platform)/        # Protected sidebar interface routes
│   │       ├── layout.tsx     # Platform shell with route-based SVG sidebar navigation
│   │       ├── dashboard/     # Grid Observability dashboard (2D Recharts, Globe.tsx)
│   │       ├── p2p/           # Energy Escrow & P2P Market (Order Book, Settlement Ledger)
│   │       └── simulation/    # Command Center (Controls, Ghosting Chart, CitySim.tsx)
├── infra/                     # Infrastructure configuration
│   └── docker-compose.yml     # Kafka and PostgreSQL service container definitions
└── project_features_summary.md # Features implementation summary
```

---

## 3. Component Details & Code Walkthrough

### A. Backend & Ingestion (`/backend`)
Written in Go, this layer serves as the simulation coordinator, WebSocket broadcaster, and database writer.
*   **Orchestrator Hub (`backend/main.go`):**
    *   Maintains a WebSocket `Hub` supporting client connections, registrations, and real-time broadcasts.
    *   Controls simulated overrides (e.g., forcing grid frequency drops, solar blockages, stress testing).
    *   Ingests telemetry via `ingestion.TelemetryStream` and spawns a Mock Trade generator to ensure the interface stays animated during demonstrations when the ML engine is offline.
    *   Consumes trade instructions from the Kafka `nexus.commands` topic and streams them to the UI client.
*   **Kafka Ingestion Pool (`backend/ingestion/kafka.go`):**
    *   Uses a buffered channel `TelemetryStream` (capacity: 20,000 events) and spawns 50 concurrent goroutine workers.
    *   Workers serialize data to JSON and write asynchronously to the Kafka broker.
    *   Uses the node's `NodeID` as the partition key to guarantee chronological ordering of messages per node.
*   **Database Schema & Models (`backend/models/`):**
    *   `telemetry.go` defines `TelemetryEvent`: `NodeID`, `NodeType` (Contributor, Consumer, Prosumer), `CurrentConsumption`, `ReplenishmentRate`, `StateOfCharge` (SoC), `LocalFrequency` (target 50.0Hz), and `Voltage`.
    *   `ledger.go` defines `P2PTrade`: `TradeID` (unique identifier), `ContributorID`, `EnergyProvidedKw`, `CreditEarned`, `GridFrequency`, and `SettledAt` timestamp.
*   **Database Connectivity (`backend/db/postgres.go`):**
    *   Configures GORM using a PostgreSQL driver and automatically migrates the `P2PTrade` table schema.

### B. Grid Arbitration Engine (`/ml`)
A lightweight decision engine built in Python to autonomously evaluate and execute trades.
*   **Arbitration Script (`ml/processor.py`):**
    *   Subscribes to `nexus.telemetry.raw` Kafka topic.
    *   Checks if local grid frequency drops below `49.8 Hz` (grid strain) and if a node's battery State of Charge (SoC) is above `50%`.
    *   If conditions are met, it triggers a `EXECUTE_TRADE` payload, proposing to discharge `10%` of that node's battery to stabilize the grid, earning `$0.15` per kW.
    *   Publishes the command back to `nexus.commands` topic.

### C. Client Frontend (`/frontend`)
Built using Next.js, Framer Motion, Recharts, and Three.js (via React Three Fiber).
*   **Landing Page (`app/page.tsx`):**
    *   Displays an immersive video background overlaid with sleek, glassmorphic menus, Framer Motion fade-ins, and links to initialize sessions.
*   **Platform Navigation Layout (`app/(platform)/layout.tsx`):**
    *   Provides a persistent 16-pixel-wide dark sidebar highlighting active route icons with emerald glow accents and overlays a faint CRT scanline effect.
*   **Grid Observability (`app/(platform)/dashboard/page.tsx`):**
    *   Streams raw websocket events, charting real-time consumption vs. replenishment.
    *   Embeds a customized 3D vector Globe (`components/Globe.tsx`) highlighting active node locations.
*   **Energy Escrow & P2P Market (`app/(platform)/p2p/page.tsx`):**
    *   Displays an active order book with real-time bid/ask depths.
    *   Lists a real-time settlement ledger showing trade IDs, contributor/consumer nodes, volume, and payouts.
    *   Provides manual `[MANUAL_BUY]` and `[MANUAL_SELL]` overrides to let users execute trades on demand.
*   **Simulation Control Center (`app/(platform)/simulation/page.tsx`):**
    *   Contains the grid sabotage and optimization dashboard.
    *   **Weather Alerts:** Preemptively triggers solar replenishment drop with a 4-second delay, warning users via alert banners.
    *   **Ghosting Analysis Chart:** Renders a Recharts line chart mapping two frequencies: *Nexus Stabilized* (actual grid) vs. *Legacy Grid* (dashed line tracking what the collapse would look like without autonomous trades).
    *   **Audio Synthesis Heartbeat:** Initiates a Web Audio API synth outputting a nominal 50Hz sine wave, shifting to a 65Hz triangle wave under load, and a grinding 30Hz sawtooth wave in emergency states.
    *   **Live Logic Terminal:** Prints calculations and grid math calculations.
*   **3D Digital Twin (`app/(platform)/simulation/components/CitySim.tsx`):**
    *   Implements a highly performant 3D city scene rendering thousands of buildings and trees using `THREE.InstancedMesh` (reduced to just two draw calls for smooth 60 FPS performance).
    *   Generates a custom window lighting texture on the fly (randomizing lights on/off).
    *   Uses raycasting (`onClick` index handling) to select specific buildings and open digital twin data overlays (battery health, core temp, earnings).
    *   Renders animated green laser energy beams connecting trade nodes when a trade occurs.
    *   Applies tilt-shift camera blur, vignette shading, and bright glowing Bloom effects that shift intensity and color based on the grid's health.

### D. Infrastructure (`/infra`)
*   **Compose Configuration (`infra/docker-compose.yml`):**
    *   Deploys single-node Confluent Kafka (`nexus-kafka` on port `9092`).
    *   Deploys PostgreSQL (`nexus-db` on port `5432`) with a persistent data volume mapped locally to `./data/postgres`.

---

## 4. Run & Test Commands

### Prerequisites
*   Docker & Docker Compose
*   Go (Golang) 1.20+
*   Node.js 18+ & npm
*   Python 3.8+ with pip

### Startup Sequence
1.  **Launch Infrastructure:**
    ```bash
    cd infra
    docker-compose up -d
    ```
2.  **Start Go Backend:**
    ```bash
    cd backend
    go run main.go
    ```
3.  **Start Python ML Processor:**
    ```bash
    cd ml
    # Create virtualenv and install requirements:
    python -m venv venv
    venv\Scripts\activate
    pip install confluent-kafka
    python processor.py
    ```
4.  **Start Next.js Frontend:**
    ```bash
    cd frontend
    npm install
    npm run dev
    ```
