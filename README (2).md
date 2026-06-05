<<<<<<< SEARCH
# Polyglot GPX Processing Scaffold

This project adds a minimal scaffold for a polyglot architecture designed to use each language where it excels.

Architecture overview and language-specific use cases:
- Rust (services/rust): high-performance GPX parsing and fast primary summaries. Use Rust for CPU-bound tasks that must process millions of track points quickly (haversine/distance, elevation accumulation, streaming parsing), produce deterministic JSON summaries, and act as the fastest-path/primary processor.
- Go (services/go): reliable API gateway, concurrency and I/O management. Use Go to accept uploads, validate/authenticate requests, implement efficient forwarding/queuing, maintain a durable SQLite-backed queue, and coordinate microservice interactions (timeouts, retries, backpressure).
- Python (services/python): rich spatial analysis and data science. Use Python for secondary/heavy-weight jobs (gpxpy parsing for detailed GPX features, geopandas/osmnx for OSM network matching, pandas for statistical summaries, shapely for geometry ops, matplotlib for plotting, and ML or heuristics for itinerary classification).
- JavaScript / Frontend (index.html, app.js, frontend/api-client.js): interactive visualization and lightweight browser processing. Use JS/Leaflet/Turf in-browser for immediate mapping, grayscale tiles, displaying live location, client-side GPX parsing for preview, and uploading to the backend; keep UI responsive and small.
- Protobuf (proto/track.proto): structured interchange for Point, TrackSegment, TrackSummary and batched DriverUpdate / TrackBatch messages, for future gRPC/Protobuf telemetry.

Quick start (dev)
- Start Rust service (primary processor): cd services/rust && cargo run
- Start Go gateway (API + queue): go run services/go/main.go
- Start Python worker (secondary analysis): python3 services/python/worker.py
- Frontend: open index.html in a dev server; the frontend expects /upload proxied to the Go gateway in development.

File descriptions
- index.html — Frontend single-page UI (Leaflet map, controls, GPX upload input, driver list & gear panel).
- styles.css — Frontend styling for the map, panels and controls.
- app.js — Frontend logic: map init, geolocation, driver polling, gear panel and GPX upload hook.
- frontend/api-client.js — Minimal client helper: uploadGpxFile(file) posts GPX to the Go gateway.
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
- frontend/app-state.js — module that centralizes shared frontend state (driverMarkers, driversLayerGroup, runtime settings).

Mermaid architecture diagram
```mermaid
flowchart LR
  Browser["Browser (index.html / app.js)"]
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