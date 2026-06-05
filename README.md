# Polyglot GPX Processing Scaffold

This project adds a minimal scaffold for a polyglot architecture:
- Rust service: high-performance GPX parsing and summary (HTTP REST).
- Go service: simple API gateway that forwards uploaded GPX files to Rust and appends summary to a queue file.
- Python worker: polls the queue file and runs a secondary (richer) spatial analysis using gpxpy, geopandas, osmnx, pandas, shapely and matplotlib; the Go gateway stores primary (fast) Rust output plus the raw GPX payload so the Python worker can run the heavier secondary analysis and save results.
- Proto: basic Protobuf schema for potential gRPC track messages.
- Frontend client: minimal JS helper to upload GPX files.

Run notes:
- Start Rust service: cd services/rust && cargo run
- Start Go gateway: go run services/go/main.go
- Start Python worker: python3 services/python/worker.py
- Frontend assumes /upload is proxied to the Go gateway in dev.