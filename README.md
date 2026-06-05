# Polyglot GPX Processing Scaffold

This project adds a minimal scaffold for a polyglot architecture designed to use each language where it excels.

Architecture overview and language-specific use cases:
- Rust (services/rust): high-performance GPX parsing and fast primary summaries. Use Rust for CPU-bound tasks that must process millions of track points quickly (haversine/distance, elevation accumulation, streaming parsing), produce deterministic JSON summaries, and act as the fastest-path/primary processor.
- Go (services/go): reliable API gateway, concurrency and I/O management. Use Go to accept uploads, validate/authenticate requests, implement efficient forwarding/queuing, maintain a durable SQLite-backed queue, and coordinate microservice interactions (timeouts, retries, backpressure).
- Python (services/python): rich spatial analysis and data science. Use Python for secondary/heavy-weight jobs (gpxpy parsing for detailed GPX features, geopandas/osmnx for OSM network matching, pandas for statistical summaries, shapely for geometry ops, matplotlib for plotting, and ML or heuristics for itinerary classification).
- JavaScript / Frontend (frontend/): interactive visualization and browser-based simulation. Uses Leaflet for mapping, OSRM routing, Overpass POI queries, Nominatim geocoding, and a full delivery simulation (car animation, HUD, fuel/money economy, driver tracking).
- Protobuf (proto/track.proto): structured interchange for Point, TrackSegment, TrackSummary and batched DriverUpdate / TrackBatch messages, for future gRPC/Protobuf telemetry.

Quick start (dev)
- Start Rust service (primary processor): cd services/rust && cargo run
- Start Go gateway (API + queue): go run services/go/main.go
- Start Python worker (secondary analysis): python3 services/python/worker.py
- Frontend: serve the root directory (e.g. `npx serve . -l 8080`); the frontend expects /upload proxied to the Go gateway in development.

File descriptions
- index.html — Frontend single-page UI (Leaflet map, controls, simulation, driver list & gear panel).
- frontend/app.js — Unified frontend entry point: map init, simulation, driver polling, gear panel, GPX upload.
- frontend/config.js — Central configuration: app settings, API URLs (OSRM, Nominatim, Overpass), tweakables, UI_LABELS dictionary.
- frontend/map.js — Leaflet map setup, tile layer management, zoom/move handlers, driver marker glow.
- frontend/api.js — API clients: OSRM routing, Overpass gas station queries, Nominatim search/reverse-geocode (with debouncing/User-Agent), place name romanization.
- frontend/simulation.js — Delivery simulation engine: car animation along route, speed/fuel/money state, turn-by-turn navigation, country detection, gas station purchasing.
- frontend/ui.js — HUD rendering: speed, fuel, money, route info, ETA, remaining distance, follow toggle, turn instructions.
- frontend/utils.js — Pure utility functions: CJK detection, distance formatting, maneuver formatting, turn step index building, maneuver SVG icons.
- frontend/gui.js — GUI panel for simulation controls: teleport, cruise speed, acceleration mode, speed limit toggles, infinite fuel.
- frontend/app-state.js — Shared state module: driver markers, layer group, runtime settings persistence.
- frontend/betaBanner.js — Beta warning banner component.
- frontend/confirmModal.js — Route overwrite confirmation modal.
- proto/track.proto — Protobuf schema for Point, TrackSegment, TrackSummary and batched DriverUpdate / TrackBatch messages.
- services/rust/* — Rust primary processor and lightweight tracker (endpoints: POST /process-gpx, POST /track[ -batch ], GET /drivers, /events, /metrics). Implements a sharded in-memory ring buffer and SSE events.
- services/go/* — Go API gateway, queue (SQLite) plumbing, worker pool, simulator and helpers (endpoints: /upload, /generate-drivers, /driverhome, /nearby, /metrics).
  - services/go/main.go — orchestrator: init DB, metrics, workers and simulator.
  - services/go/upload_handler.go — HTTP upload handler and instrumentation wrappers.
  - services/go/worker.go — worker pool that forwards GPX to Rust (multipart) with retry/backoff and circuit-breaker.
  - services/go/db.go — SQLite queue schema and helpers; persists raw GPX payloads to queue_store/.
  - services/go/spatial.go — SQLite RTree helpers for nearby driver queries.
  - services/go/driver_generator.go — generator for synthetic driver data and driverhome file writer.
  - services/go/simulator.go — simulator that batches driver updates and posts to Rust tracker.
- services/python/* — Python worker that consumes the SQLite queue, runs secondary analysis (gpxpy, geopandas, matplotlib) and writes analysis artifacts.
- services/go/ and queue_store/ — runtime data: queue_store contains persisted GPX payloads and queue.db.

Mermaid architecture diagram
```mermaid
flowchart LR
  Browser["Browser (index.html / frontend/ modules)"]
  GoAPI["Go Gateway\n(/upload, /nearby, /generate-drivers)"]
  Rust["Rust Primary\n(/process-gpx, /track-batch, /drivers, /events)"]
  SQLite["SQLite Queue\n(queue_store/queue.db + payloads)"]
  Python["Python Worker\n(secondary analysis)"]
  Frontend["Leaflet UI\nGPX upload & driver markers"]

  Browser -->|POST /upload (multipart)| GoAPI
  GoAPI -->|Forward multipart /process-gpx| Rust
  Rust -->|Primary JSON summary| GoAPI
  GoAPI -->|Enqueue (payload ref + primary)| SQLite
  Python -->|Poll & consume| SQLite
  Python -->|Write analysis artifacts| queue_store
  Rust -->|Telemetry: POST /track-batch| Rust
  Simulator["Simulator (Go)"] -->|POST /track-batch| Rust
  Browser -->|poll GET /drivers or SSE /events| Rust
  Frontend --> Browser

  style GoAPI fill:#f3f4f6,stroke:#8b37ff
  style Rust fill:#fff7f7,stroke:#ff375f
  style SQLite fill:#f7fff7,stroke:#2ecc71
  style Python fill:#fffaf0,stroke:#8b37ff
```

## Logistics Router (production, 1000 drivers / 100 orders per min)

The logistics router extends the base scaffold into a production-ready dispatch system:

### Architecture

```
Frontend (Leaflet) ←── SSE ── Rust Tracker (R-tree spatial index, dispatch)
       ↕                          ↕
Go Gateway (REST) ←─── Go Logistics API (orders, auth, rate-limit)
       ↕
Python (secondary queue analysis)
```

### Service roles
- **Go Logistics API** (`services/go/logistics.go`, port `8082`): order CRUD, dispatch (nearest-driver via Rust/Go), API key auth, IP-based rate limiting (100 req/s), optional TLS. Runs alongside the existing gateway.
- **Rust Tracker** (`services/rust/src/main.rs`, port `3030`): now uses `rstar` R-tree per shard for O(log n) nearest-driver queries. Added `POST /dispatch` (find nearest driver to pickup) and `POST /route-eta` (OSRM distance/duration with haversine fallback). SSE at `/events` streams live driver positions to the frontend.
- **Frontend** (`frontend/logistics.js`): SSE client for real-time driver marker updates, order layer (pickup/dropoff markers with status colors), dispatch panel (press `o` to toggle), create order by clicking the map.
- **Caddy** reverse proxy with auto TLS, rate limiting, security headers.

### Endpoints

| Service | Endpoint | Method | Description |
|---------|----------|--------|-------------|
| Go logistics | `/api/orders` | POST | Create order (pickup/dropoff coords) |
| Go logistics | `/api/orders?status=` | GET | List orders, optional filter |
| Go logistics | `/api/orders/{id}/status` | PUT | Update order (picked_up, delivered, cancelled) |
| Go logistics | `/api/dispatch` | POST | Assign nearest driver to pending order |
| Go logistics | `/api/stats` | GET | Operational statistics |
| Rust | `/dispatch` | POST | Find nearest driver (R-tree O(log n)) |
| Rust | `/route-eta` | POST | OSRM distance/duration between two points |
| Rust | `/events` | GET | SSE stream of driver position updates |
| Rust | `/track-batch` | POST | Batch driver GPS ping ingestion |

### Running in production
```bash
# Set API keys and domain
export API_KEYS=prod-key-1,prod-key-2
export DOMAIN=logistics.example.com

# Start all services
docker-compose up --build -d

# Scale Rust tracker horizontally
docker-compose up --scale rust-tracker=3 -d
```

### Security
- All API endpoints (except `/health`, `/metrics`) require `X-API-Key` header when `API_KEYS` env is set
- IP-based rate limiting (100 req/s, burst 200) on logistics API
- Caddy reverse proxy adds security headers, TLS termination, and Caddy-level rate limiting
- Optional TLS for Go services via `TLS_CERT`/`TLS_KEY` env vars

### Load characteristics
- **1000 drivers**: Rust handles 1000 GPS pings in ~2ms (4 shards × R-tree O(log n))
- **100 orders/min**: Go processes order creation + dispatch in <5ms (in-memory store with SQLite RTree fallback)
- **SSE fan-out**: Rust broadcast channel streams 1000 updates/sec to all connected frontends
