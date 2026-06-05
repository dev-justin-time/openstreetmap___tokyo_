# Missing Components & High-Value Enhancements

This document lists technologies, logic, and concrete high-value patterns that are not present in the scaffold but would significantly improve correctness, performance, observability, security, and maintainability.

## Summary: key missing areas
- Reliable queue / durable storage (S3, SQLite/WAL, Kafka, Redis streams)
- Proper multipart handling and validation between services
- Authn/Authz, TLS and rate-limiting for public endpoints
- Deterministic sharding / service discovery for multi-instance Rust trackers
- Backpressure, retries and circuit-breaker when forwarding to processors
- Metrics, tracing and health checks (Prometheus + OpenTelemetry)
- Batching, downsampling and spatial indexing for heavy telemetry
- Efficient binary formats for telemetry (Protobuf/gRPC with streaming) and compression (zstd)
- Tests, CI and fuzzing for parser correctness

## Architecture / Logic improvements (concrete)
1. Durable queue pattern
   - Replace queue.log with either:
     - Append-only object storage (S3) + metadata queue (SQLite/Redis)
     - Lightweight local queue: SQLite with sequential IDs and status column (pending/processing/done)
   - Example structure (SQLite table):
     - queue(id INTEGER PRIMARY KEY AUTOINCREMENT, created_at, payload_ref TEXT, primary_json JSON, status TEXT, attempts INT)

2. Proper multipart forwarding & validation
   - Use multipart.Writer in Go to forward file to Rust endpoint as a real file part.
   - Validate file size, mime type, and run a lightweight GPX sanity check before forwarding.

3. Backpressure and retries
   - Implement bounded worker pool when forwarding to Rust; on failure, enqueue and retry with exponential backoff and capped attempts.
   - Use circuit-breaker to avoid saturating Rust service under load.

4. Deterministic sharding & service discovery
   - Use consistent hashing with configurable shard ring (e.g., jump-consistent-hash or rendezvous hashing) and env/config map of Rust instance addresses instead of assuming sequential localhost ports.

5. Telemetry ingress optimizations
   - Batch incoming driver updates into groups (e.g., 50 updates per POST) to reduce HTTP overhead.
   - Use Protobuf/gRPC streaming endpoints for high-rate telemetry; fallback to HTTP/JSON for small scale.

6. Spatial indexing and downsampling
   - Use R-tree or geohash index (on-disk or in-memory) to quickly query nearby drivers.
   - For long GPX traces, run Douglas-Peucker or time-based downsampling before heavy processing.

7. Efficient in-memory driver store
   - Replace VecDeque+HashMap eviction with an indexed LRU or time-indexed ring buffer supporting TTL and O(1) removals.
   - Optionally shard state internally (N shards) to reduce lock contention.

8. Observability and tracing
   - Add Prometheus metrics for requests, processing latency, queue depth, dropped updates, and gos/rust worker metrics.
   - Add distributed tracing headers (W3C Trace Context) and OpenTelemetry instrumentation.

## High-value logic examples

1) SQLite queue pseudo-SQL
   - CREATE TABLE queue (id INTEGER PRIMARY KEY, created_at INTEGER, payload_ref TEXT, primary_json TEXT, status TEXT DEFAULT 'pending', attempts INTEGER DEFAULT 0);
   - SELECT id, payload_ref FROM queue WHERE status='pending' ORDER BY id LIMIT 10 FOR UPDATE;

2) Multipart-forwarding (Go, sketch)
   - Use multipart.NewWriter, create form file part "gpx", copy contents, close writer, set Content-Type to writer.FormDataContentType() for the POST to Rust.

3) Batched telemetry post (pseudo-Rust endpoint)
   - Accept TrackBatch { repeated DriverUpdate updates }
   - Process updates in a tight loop, insert into internal shard buffer, send single broadcast per update or per batch.

4) Retry/backoff (concept)
   - On transient failure, push queue entry with attempts++, schedule next attempt after backoff = min(base*(2^attempts), maxBackoff). Persist attempt count.

5) Consistent hashing for shard selection (sketch)
   - Maintain list of rust instance addresses, compute hash(id) -> ring position -> choose instance. Use library or jump hash to reassign minimally on topology change.

## Performance upgrades & expected impact
- gRPC streaming + Protobuf: reduce CPU parsing overhead and shrink payloads (20-50% bandwidth savings).
- Batching telemetry: reduce HTTP calls and context-switches (improve throughput by 5-10x depending on batch size).
- SQLite queue vs. queue.log: enables atomic writes, easy backfill/retry and safe multi-consumer processing.
- Spatial index + downsampling: reduce Python/Rust heavy work by filtering 70-95% of redundant points before expensive ops.
- Sharded in-memory store + lock striping: reduce contention and increase concurrent update throughput (linear-ish scale with shards).

## Security & operational checklist
- Enforce TLS for all inter-service and external endpoints.
- Add API keys / JWT and restrict simulator endpoints to internal networks.
- Cap file sizes, validate GPX schema, and sanitize NDJSON entries.
- Run periodic background compaction of queues and retention cleanup.
- Limit memory growth with soft quotas and eviction policies.

## Testing & CI recommendations
- Add unit tests for GPX parsing edge cases (missing timestamps, zero-length segments).
- Add integration tests: submit GPX -> Rust summary -> Go queue -> Python worker processing stub.
- Add load test harness for telemetry ingestion (k6 or vegeta) to validate batching/sharding.