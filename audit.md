# Audit: GPX Polyglot Processing Scaffold

Purpose
- Validate architecture, claims, and key data flow for the polyglot GPX processing scaffold using Go, Rust, Python and JavaScript.

Architecture summary
- Rust: primary/high-performance GPX processor (streaming parse, distance/elevation), exposes HTTP endpoints (POST /process-gpx) and a lightweight tracker (POST /track, GET /drivers, GET /events).
- Go: API gateway and queue manager; accepts uploads at /upload, forwards to Rust primary, appends NDJSON queue entries for Python workers, runs simulators and helper endpoints (/generate-drivers, /driverhome).
- Python: secondary worker that polls the queue, performs rich spatial analysis (gpxpy, geopandas, osmnx, pandas, shapely), and writes artifact PNGs/JSON and stored reports.
- JavaScript: frontend UI (Leaflet) with GPX upload, live driver markers, SSE/HTTP polling to Rust, and local persistence.

Claims validated
- "Rust for raw speed": The Rust service uses warp + gpx crates and is designed to parse and summarize GPX quickly; it is the correct choice for CPU-bound streaming parsing and fast summaries.
- "Go for concurrency/networking": Go acts as the upload gateway, queue appender, and simulator generator; its concurrency model and small binary footprint suit this role.
- "Python for spatial analysis": secondary analysis uses gpxpy/geopandas/osmnx/pandas/shapely/matplotlib — appropriate libraries for OSM matching and plotting.
- "JS for visualization": Frontend uses Leaflet and in-browser hooks to upload GPX and display driver markers; this is fit-for-purpose.

Security & operational notes
- The current pipeline appends raw GPX into a plaintext queue.log; production should:
  - Sanitize inputs, enforce file size/type limits, and authenticate uploads (TLS + auth).
  - Avoid embedding raw file bytes directly into JSON without proper encoding (we currently escape quotes/newlines).
  - Consider using object storage (S3) for raw files and only queue references.
- The Go gateway forwards to Rust at a fixed localhost address; deployment must route to actual service addresses and handle timeouts/retries/backpressure.
- The queue is NDJSON in the local filesystem (queue.log); for scale use durable queue systems (Kafka/Redis/Cloud queues) and transactional writes.

Functional checks performed (against repo)
- Fixed a syntax/compile-time issue in services/go/main.go: missing strconv import and an extra brace after escapeForJSON led to JS upload failing with a server-side error that manifested as an unexpected token on the frontend.
- Confirmed frontend app.js uses dynamic import for frontend/api-client.js and displays JSON responses; ensured the GPX file input hook posts to /upload which the Go gateway processes.
- Verified Rust tracker code implements sharding via INSTANCE_INDEX / INSTANCE_COUNT and uses an in-memory cap (500) with eviction; simulator in Go posts to ports 3030.. based on stable FNV-ish hashing.

Recommendations
- Replace plaintext queue.log with a durable queue or object storage + metadata queue.
- Add authentication/authorization to /upload and simulator endpoints; never expose internal simulator endpoints publicly.
- Harden input parsing (validate GPX before forwarding) and use content-type multipart/form-data boundaries correctly when forwarding to Rust.
- Add monitoring/metrics (requests/sec, processing latency, queue lag, worker success/failure).
- For large-scale trace matching, pre-filter points (downsample/simplify) before expensive OSM matching, or use tiled spatial indices.

Next steps (if you want me to implement)
- Replace NDJSON queue.log with SQLite or lightweight message broker.
- Add auth token check to /upload and a simple admin UI to view queue size.
- Implement proper multipart forwarding using multipart.Writer so Rust receives a proper file upload boundary.