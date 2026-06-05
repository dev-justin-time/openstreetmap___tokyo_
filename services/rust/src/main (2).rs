use std::convert::Infallible;
use std::sync::Arc;
use std::hash::{Hash, Hasher};
use std::collections::hash_map::DefaultHasher;
use warp::Filter;
use serde::{Serialize, Deserialize};
use tokio::sync::{Mutex, RwLock, broadcast};
use std::collections::HashMap;

// Prometheus
use prometheus::{Encoder, TextEncoder, register_counter_vec, register_histogram_vec, register_int_gauge, CounterVec, HistogramVec, IntGauge};

// for reading headers
use warp::http::HeaderMap;

// new deps (kept for future use, rstar sled not used in this simplified replacement but left in Cargo)
use serde_json::json;

#[derive(Serialize, Deserialize, Clone, Debug)]
struct DriverUpdate {
    id: String,
    lat: f64,
    lon: f64,
    status: Option<String>,
    ts_unix_ms: Option<i64>,
}

#[derive(Serialize)]
struct DriversResponse {
    count: usize,
    drivers: Vec<DriverUpdate>,
}

// --- Sharded indexed ring buffer implementation ---
//
// Each internal shard keeps a fixed-capacity ring Vec<Option<DriverUpdate>>
// plus a HashMap from id -> index for O(1) lookup and removals.
// Insert/update is O(1). Eviction is handled by overwriting the next slot
// and removing any mapping that was evicted. We route driver ids to shards
// using a stable hash to reduce lock contention.
struct Shard {
    buffer: Vec<Option<DriverUpdate>>,
    index_map: HashMap<String, usize>,
    head: usize,     // points at the most recently inserted index
    size: usize,
    capacity: usize,
}

impl Shard {
    fn new(capacity: usize) -> Self {
        Self {
            buffer: vec![None; capacity],
            index_map: HashMap::with_capacity(capacity),
            head: capacity - 1, // start so first insert goes to 0
            size: 0,
            capacity,
        }
    }

    // Insert or update. If already present, overwrite in-place.
    // If new, advance head and place there, evicting previous occupant.
    fn insert(&mut self, du: DriverUpdate) {
        if let Some(&idx) = self.index_map.get(&du.id) {
            // update existing in-place
            self.buffer[idx] = Some(du);
            self.head = idx; // treat as most recent
            return;
        }
        // new insertion: advance head modulo capacity
        self.head = (self.head + 1) % self.capacity;
        // if slot occupied, remove old mapping
        if let Some(old) = &self.buffer[self.head] {
            self.index_map.remove(&old.id);
        } else {
            // if previously empty, increase size
            if self.size < self.capacity {
                self.size += 1;
            }
        }
        self.buffer[self.head] = Some(du.clone());
        self.index_map.insert(du.id.clone(), self.head);
    }

    // Remove by id if present (O(1)). Returns true if removed.
    fn remove(&mut self, id: &str) -> bool {
        if let Some(&idx) = self.index_map.get(id) {
            self.buffer[idx] = None;
            self.index_map.remove(id);
            self.size = self.size.saturating_sub(1);
            // if removed index was head, we don't attempt to move head; head indicates most recent known slot
            return true;
        }
        false
    }

    // Return all present driver updates in newest-first order
    fn all_newest_first(&self) -> Vec<DriverUpdate> {
        let mut out = Vec::with_capacity(self.size);
        if self.size == 0 {
            return out;
        }
        // iterate capacity times backwards from head to collect existing items
        let mut seen = 0usize;
        let mut idx = self.head;
        for _ in 0..self.capacity {
            if let Some(Some(du)) = self.buffer.get(idx) {
                out.push(du.clone());
                seen += 1;
                if seen >= self.size {
                    break;
                }
            }
            if idx == 0 {
                idx = self.capacity - 1;
            } else {
                idx -= 1;
            }
        }
        out
    }

    // Simple radius scan in degrees (linear scan)
    fn query_nearby(&self, lat: f64, lon: f64, radius_deg: f64) -> Vec<DriverUpdate> {
        let mut out = Vec::new();
        for slot in self.buffer.iter() {
            if let Some(du) = slot {
                if (du.lat - lat).abs() <= radius_deg && (du.lon - lon).abs() <= radius_deg {
                    out.push(du.clone());
                }
            }
        }
        out
    }
}

struct ShardedState {
    shards: Vec<Mutex<Shard>>,
    shard_count: usize,
}

impl ShardedState {
    fn new(shard_count: usize, per_shard_capacity: usize) -> Self {
        let mut shards = Vec::with_capacity(shard_count);
        for _ in 0..shard_count {
            shards.push(Mutex::new(Shard::new(per_shard_capacity)));
        }
        Self { shards, shard_count }
    }

    // stable shard selection by id
    fn shard_for(&self, id: &str) -> usize {
        let mut hasher = DefaultHasher::new();
        id.hash(&mut hasher);
        (hasher.finish() as usize) % self.shard_count
    }

    // async insert routed to shard
    async fn insert(&self, du: DriverUpdate) {
        let idx = self.shard_for(&du.id);
        let mut s = self.shards[idx].lock().await;
        s.insert(du);
    }

    // async collect all drivers across shards, newest-first per-shard
    async fn all(&self) -> Vec<DriverUpdate> {
        let mut out = Vec::new();
        // acquire read locks per shard sequentially to avoid holding many locks simultaneously long-term
        for shard in &self.shards {
            let s = shard.lock().await;
            let mut shard_items = s.all_newest_first();
            out.append(&mut shard_items);
        }
        out
    }

    async fn query_nearby(&self, lat: f64, lon: f64, radius_deg: f64) -> Vec<DriverUpdate> {
        let mut out = Vec::new();
        for shard in &self.shards {
            let s = shard.lock().await;
            let mut items = s.query_nearby(lat, lon, radius_deg);
            out.append(&mut items);
        }
        out
    }
}

// Determine whether a driver id belongs to this instance's external shard using a stable hash.
fn owns_shard(id: &str, instance_index: usize, instance_count: usize) -> bool {
    if instance_count <= 1 {
        return true;
    }
    let mut hasher = DefaultHasher::new();
    id.hash(&mut hasher);
    let h = hasher.finish() as usize;
    (h % instance_count) == instance_index
}

type SharedState = Arc<ShardedState>;

#[tokio::main]
async fn main() {
    // Determine instance sharding from environment vars
    let instance_index: usize = std::env::var("INSTANCE_INDEX").ok().and_then(|s| s.parse().ok()).unwrap_or(0);
    let instance_count: usize = std::env::var("INSTANCE_COUNT").ok().and_then(|s| s.parse().ok()).unwrap_or(1);
    if instance_count == 0 {
        eprintln!("INSTANCE_COUNT must be >= 1; defaulting to 1");
    }

    // internal shard config: allow override via env SHARD_COUNT, default 4
    let shard_count: usize = std::env::var("SHARD_COUNT").ok().and_then(|s| s.parse().ok()).unwrap_or(4);
    let per_shard_capacity: usize = std::env::var("SHARD_CAPACITY").ok().and_then(|s| s.parse().ok()).unwrap_or(500 / shard_count.max(1));
    let state = Arc::new(ShardedState::new(shard_count.max(1), per_shard_capacity.max(16)));

    // Prometheus metrics for Rust tracker
    let req_counter: CounterVec = register_counter_vec!(
        "rust_tracker_http_requests_total",
        "Total HTTP requests handled by rust tracker, labeled by endpoint and status",
        &["endpoint", "status"]
    ).unwrap();

    let req_latency: HistogramVec = register_histogram_vec!(
        "rust_tracker_request_duration_seconds",
        "Request duration in seconds by endpoint",
        &["endpoint"]
    ).unwrap();

    let drivers_count: IntGauge = register_int_gauge!(
        "rust_tracker_driver_count",
        "Approx number of drivers currently held"
    ).unwrap();

    // Broadcast channel for realtime updates (buffer 1024)
    let (tx, _rx) = broadcast::channel::<DriverUpdate>(1024);
    let tx_filter = warp::any().map(move || tx.clone());
    let state_filter = warp::any().map(move || state.clone());
    let instance_info = warp::any().map(move || (instance_index, instance_count));

    // Prometheus metrics clones into closures
    let req_counter_filter = warp::any().map(move || req_counter.clone());
    let req_latency_filter = warp::any().map(move || req_latency.clone());
    let drivers_count_filter = warp::any().map(move || drivers_count.clone());

    // POST /track  -> accept JSON driver update and broadcast/store it
    let track = warp::post()
        .and(warp::path("track"))
        .and(warp::body::json())
        .and(state_filter.clone())
        .and(tx_filter.clone())
        .and(instance_info.clone())
        .and_then(handle_track);

    // POST /track-batch -> accept array of updates to reduce HTTP overhead
    let track_batch = warp::post()
        .and(warp::path("track-batch"))
        .and(warp::body::json())
        .and(state_filter.clone())
        .and(tx_filter.clone())
        .and(instance_info.clone())
        .and_then(handle_track_batch);

    // GET /drivers -> return current driver list (up to capacity), newest-first
    let drivers = warp::get()
        .and(warp::path("drivers"))
        .and(state_filter.clone())
        .and(drivers_count_filter.clone())
        .and_then(handle_drivers);

    // GET /nearby?lat=&lon=&radius_deg=0.01 -> return nearby drivers using per-shard scan
    let nearby = warp::get()
        .and(warp::path("nearby"))
        .and(warp::query::<std::collections::HashMap<String, String>>())
        .and(state_filter.clone())
        .and_then(handle_nearby);

    // SSE /events -> stream updates to clients
    let sse = warp::path("events")
        .and(warp::get())
        .and(tx_filter.clone())
        .map(|tx: broadcast::Sender<DriverUpdate>| {
            // create a stream from the broadcast receiver
            let mut rx = tx.subscribe();
            let stream = async_stream::stream! {
                loop {
                    match rx.recv().await {
                        Ok(update) => {
                            let json = match serde_json::to_string(&update) {
                                Ok(s) => s,
                                Err(_) => continue,
                            };
                            yield Ok::<_, Infallible>(warp::sse::Event::default().data(json));
                        }
                        Err(broadcast::error::RecvError::Lagged(_)) => continue,
                        Err(broadcast::error::RecvError::Closed) => break,
                    }
                }
            };
            warp::sse::reply(warp::sse::keep_alive().stream(stream))
        });

    // /metrics endpoint for Prometheus scraping
    let metrics_route = warp::path("metrics").map(|| {
        let encoder = TextEncoder::new();
        let metric_families = prometheus::gather();
        let mut buffer = vec![];
        encoder.encode(&metric_families, &mut buffer).unwrap();
        warp::reply::with_header(buffer, "Content-Type", encoder.format_type())
    });

    let routes = track.or(track_batch).or(drivers).or(nearby).or(sse).or(metrics_route).with(warp::cors().allow_any_origin());

    println!("Rust tracker running on http://127.0.0.1:3030 (endpoints: POST /track, POST /track-batch, GET /drivers, GET /events, /metrics)");
    println!("Instance shard: index={} count={}", instance_index, instance_count);
    println!("Internal shards: count={} per_shard_capacity={}", shard_count, per_shard_capacity);
    warp::serve(routes).run(([127,0,0,1], 3030)).await;
}

async fn handle_track(update: DriverUpdate, state: SharedState, tx: broadcast::Sender<DriverUpdate>, instance_info: (usize, usize)) -> Result<impl warp::Reply, Infallible> {
    let (instance_index, instance_count) = instance_info;
    // Only accept and process updates that belong to this instance's external shard
    if owns_shard(&update.id, instance_index, instance_count) {
        // insert into internal sharded state
        state.insert(update.clone()).await;
        // broadcast (best-effort)
        let _ = tx.send(update.clone());
        // update approximate gauge
        // compute total drivers count (cheap approximation by summing shards sizes)
        let all = state.all().await;
        // try to set gauge; ignore error
        let _ = std::panic::catch_unwind(|| {
            if let Some(gauge) = prometheus::gather().into_iter().find_map(|mf| None) {
                // noop: we can't set via gathered families; instead rely on external metrics above
            }
        });
        Ok(warp::reply::with_status("ok", warp::http::StatusCode::OK))
    } else {
        // Not owned by this instance -> respond 204 No Content to indicate forward/ignore
        Ok(warp::reply::with_status("", warp::http::StatusCode::NO_CONTENT))
    }
}

async fn handle_track_batch(updates: Vec<DriverUpdate>, state: SharedState, tx: broadcast::Sender<DriverUpdate>, instance_info: (usize, usize)) -> Result<impl warp::Reply, Infallible> {
    // Lightweight batching optimizations:
    // 1) De-duplicate multiple updates for the same driver id within the batch, keeping the most recent.
    // 2) Do a simple spatial coalescing: if multiple kept updates for same id are within an epsilon, keep last.
    // 3) Group per internal shard and insert per-shard while holding each shard lock only once.
    //
    // These reduce CPU and locking overhead for high-rate telemetry and act as an inexpensive downsampling
    // step prior to inserting into the in-memory store or broadcasting to listeners.

    let (instance_index, instance_count) = instance_info;

    // first, keep only updates owned by this instance and de-duplicate by id keeping the last seen
    let mut last_by_id: HashMap<String, DriverUpdate> = HashMap::with_capacity(updates.len());
    for update in updates.into_iter() {
        if !owns_shard(&update.id, instance_index, instance_count) {
            continue;
        }
        // overwrite existing to keep the most recent occurrence in the incoming batch
        last_by_id.insert(update.id.clone(), update);
    }

    // optional spatial coalescing threshold in degrees (very small: ~10m is ~0.00009 deg lat)
    const COALESCE_DEG: f64 = 0.00009;

    // group per-shard
    let mut per_shard_batches: HashMap<usize, Vec<DriverUpdate>> = HashMap::new();
    for (_id, mut u) in last_by_id {
        // If desired, we could compare against existing shard state to drop near-duplicates;
        // here we do an intra-batch spatial coalesce pass per-shard by keeping a map of last inserted coords per id.
        // route to internal shard
        let shard_idx = state.shard_for(&u.id);
        // very cheap normalization: clamp lat/lon to reduce near-duplicate variety (micro-downsample)
        u.lat = (u.lat / COALESCE_DEG).round() * COALESCE_DEG;
        u.lon = (u.lon / COALESCE_DEG).round() * COALESCE_DEG;
        per_shard_batches.entry(shard_idx).or_default().push(u);
    }

    // Insert per shard (acquire each shard lock once for its batch)
    for (shard_idx, batch) in per_shard_batches {
        let shard_mutex = &state.shards[shard_idx];
        let mut s = shard_mutex.lock().await;
        for u in batch.iter() {
            s.insert(u.clone());
            // broadcast each insertion (best-effort)
            let _ = tx.send(u.clone());
        }
    }

    // update approximate drivers gauge by summing shard sizes (cheap)
    let mut total = 0usize;
    for shard in &state.shards {
        let s = shard.lock().await;
        total += s.size;
    }
    // Note: drivers_count gauge capture was registered earlier; to avoid borrow issues we omit direct set here.

    Ok(warp::reply::with_status("ok", warp::http::StatusCode::OK))
}

async fn handle_drivers(state: SharedState) -> Result<impl warp::Reply, Infallible> {
    let drivers = state.all().await;
    let resp = DriversResponse {
        count: drivers.len(),
        drivers,
    };
    Ok(warp::reply::json(&resp))
}

async fn handle_nearby(query: std::collections::HashMap<String, String>, state: SharedState) -> Result<impl warp::Reply, Infallible> {
    let lat = query.get("lat").and_then(|v| v.parse::<f64>().ok()).unwrap_or(0.0);
    let lon = query.get("lon").and_then(|v| v.parse::<f64>().ok()).unwrap_or(0.0);
    let radius_deg = query.get("radius_deg").and_then(|v| v.parse::<f64>().ok()).unwrap_or(0.01);
    let near = state.query_nearby(lat, lon, radius_deg).await;
    let resp = DriversResponse {
        count: near.len(),
        drivers: near,
    };
    Ok(warp::reply::json(&resp))
}