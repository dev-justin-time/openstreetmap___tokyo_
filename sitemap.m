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