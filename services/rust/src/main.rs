use std::convert::Infallible;
use std::sync::Arc;
use std::hash::{Hash, Hasher};
use std::collections::hash_map::DefaultHasher;
use warp::Filter;
use serde::{Serialize, Deserialize};
use tokio::sync::{Mutex, broadcast};
use std::collections::HashMap;

use rstar::RTree;
use rstar::RTreeObject;
use rstar::AABB;

use prometheus::{Encoder, TextEncoder, register_counter_vec, register_histogram_vec, register_int_gauge, CounterVec, HistogramVec, IntGauge};

// ─── Data types ─────────────────────────────────────────────────────────────

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

#[derive(Serialize, Deserialize, Clone, Debug)]
struct OrderDispatchRequest {
    pickup_lat: f64,
    pickup_lon: f64,
    radius_m: f64,
}

#[derive(Serialize, Clone, Debug)]
struct DispatchResult {
    driver_id: String,
    driver_lat: f64,
    driver_lon: f64,
    distance_m: f64,
}

#[derive(Serialize, Deserialize, Clone, Debug)]
struct RouteETAQuery {
    lat1: f64,
    lon1: f64,
    lat2: f64,
    lon2: f64,
}

#[derive(Serialize, Clone, Debug)]
struct RouteETAResult {
    distance_m: f64,
    duration_sec: f64,
}

// ─── R-tree spatial index ───────────────────────────────────────────────────

#[derive(Clone, PartialEq)]
struct SpatialPoint {
    id: String,
    lat: f64,
    lon: f64,
    status: String,
}

impl RTreeObject for SpatialPoint {
    type Envelope = AABB<[f64; 2]>;

    fn envelope(&self) -> Self::Envelope {
        AABB::from_point([self.lon, self.lat])
    }
}

impl rstar::PointDistance for SpatialPoint {
    fn distance_2(&self, point: &[f64; 2]) -> f64 {
        let dx = self.lon - point[0];
        let dy = self.lat - point[1];
        dx * dx + dy * dy
    }
}

// ─── Sharded state with R-tree per shard ────────────────────────────────────

struct Shard {
    rtree: RTree<SpatialPoint>,
    index_map: HashMap<String, SpatialPoint>,
    capacity: usize,
}

impl Shard {
    fn new(capacity: usize) -> Self {
        Self {
            rtree: RTree::new(),
            index_map: HashMap::with_capacity(capacity),
            capacity,
        }
    }

    fn insert(&mut self, du: DriverUpdate) {
        let sp = SpatialPoint {
            id: du.id.clone(),
            lat: du.lat,
            lon: du.lon,
            status: du.status.clone().unwrap_or_default(),
        };
        // Remove old point if exists
        if let Some(old) = self.index_map.get(&du.id) {
            self.rtree.remove(old);
        }
        self.rtree.insert(sp.clone());
        self.index_map.insert(du.id.clone(), sp);
    }

    fn nearest(&self, lat: f64, lon: f64, radius_m: f64) -> Vec<DispatchResult> {
        let radius_deg = radius_m / 111_320.0;
        let results: Vec<DispatchResult> = self.rtree
            .locate_within_distance([lon, lat], radius_deg * radius_deg)
            .map(|sp| {
                let d = haversine(lat, lon, sp.lat, sp.lon);
                DispatchResult {
                    driver_id: sp.id.clone(),
                    driver_lat: sp.lat,
                    driver_lon: sp.lon,
                    distance_m: d,
                }
            })
            .collect();
        results
    }

    fn all(&self) -> Vec<DriverUpdate> {
        self.index_map.values().map(|sp| DriverUpdate {
            id: sp.id.clone(),
            lat: sp.lat,
            lon: sp.lon,
            status: Some(sp.status.clone()),
            ts_unix_ms: None,
        }).collect()
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

    fn shard_for(&self, id: &str) -> usize {
        let mut hasher = DefaultHasher::new();
        id.hash(&mut hasher);
        (hasher.finish() as usize) % self.shard_count
    }

    async fn insert(&self, du: DriverUpdate) {
        let idx = self.shard_for(&du.id);
        let mut s = self.shards[idx].lock().await;
        s.insert(du);
    }

    async fn all(&self) -> Vec<DriverUpdate> {
        let mut out = Vec::new();
        for shard in &self.shards {
            let s = shard.lock().await;
            out.append(&mut s.all());
        }
        out
    }

    async fn nearest_driver(&self, lat: f64, lon: f64, radius_m: f64) -> Option<DispatchResult> {
        let mut best: Option<DispatchResult> = None;
        for shard in &self.shards {
            let s = shard.lock().await;
            for r in s.nearest(lat, lon, radius_m) {
                match &best {
                    None => best = Some(r),
                    Some(b) if r.distance_m < b.distance_m => best = Some(r),
                    _ => {}
                }
            }
        }
        best
    }
}

fn haversine(lat1: f64, lon1: f64, lat2: f64, lon2: f64) -> f64 {
    let r = 6371000.0;
    let dlat = (lat2 - lat1).to_radians();
    let dlon = (lon2 - lon1).to_radians();
    let a = (dlat / 2.0).sin().powi(2)
        + lat1.to_radians().cos() * lat2.to_radians().cos() * (dlon / 2.0).sin().powi(2);
    r * 2.0 * a.sqrt().asin()
}

fn owns_shard(id: &str, instance_index: usize, instance_count: usize) -> bool {
    if instance_count <= 1 { return true; }
    let mut hasher = DefaultHasher::new();
    id.hash(&mut hasher);
    (hasher.finish() as usize % instance_count) == instance_index
}

type SharedState = Arc<ShardedState>;

#[tokio::main]
async fn main() {
    let instance_index: usize = std::env::var("INSTANCE_INDEX").ok().and_then(|s| s.parse().ok()).unwrap_or(0);
    let instance_count: usize = std::env::var("INSTANCE_COUNT").ok().and_then(|s| s.parse().ok()).unwrap_or(1);
    let shard_count: usize = std::env::var("SHARD_COUNT").ok().and_then(|s| s.parse().ok()).unwrap_or(4);
    let per_shard_capacity: usize = std::env::var("SHARD_CAPACITY").ok().and_then(|s| s.parse().ok()).unwrap_or(500 / shard_count.max(1));
    let state = Arc::new(ShardedState::new(shard_count.max(1), per_shard_capacity.max(16)));

    // Prometheus metrics
    let _req_counter: CounterVec = register_counter_vec!(
        "rust_tracker_http_requests_total",
        "Total HTTP requests handled by rust tracker",
        &["endpoint", "status"]
    ).unwrap();
    let _req_latency: HistogramVec = register_histogram_vec!(
        "rust_tracker_request_duration_seconds",
        "Request duration in seconds by endpoint",
        &["endpoint"]
    ).unwrap();
    let _drivers_count: IntGauge = register_int_gauge!(
        "rust_tracker_driver_count",
        "Approx number of drivers currently held"
    ).unwrap();
    let dispatch_counter: CounterVec = register_counter_vec!(
        "rust_dispatch_requests_total",
        "Dispatch request count by result",
        &["result"]
    ).unwrap();

    let (tx, _rx) = broadcast::channel::<DriverUpdate>(1024);
    let tx_filter = warp::any().map(move || tx.clone());
    let state_filter = warp::any().map(move || state.clone());
    let instance_info = warp::any().map(move || (instance_index, instance_count));

    // POST /track
    let track = warp::post()
        .and(warp::path("track"))
        .and(warp::body::json())
        .and(state_filter.clone())
        .and(tx_filter.clone())
        .and(instance_info.clone())
        .and_then(handle_track);

    // POST /track-batch
    let track_batch = warp::post()
        .and(warp::path("track-batch"))
        .and(warp::body::json())
        .and(state_filter.clone())
        .and(tx_filter.clone())
        .and(instance_info.clone())
        .and_then(handle_track_batch);

    // GET /drivers
    let drivers = warp::get()
        .and(warp::path("drivers"))
        .and(state_filter.clone())
        .and_then(handle_drivers);

    // GET /nearby
    let nearby = warp::get()
        .and(warp::path("nearby"))
        .and(warp::query::<std::collections::HashMap<String, String>>())
        .and(state_filter.clone())
        .and_then(handle_nearby);

    // POST /dispatch — nearest driver to pickup
    let dispatch_route = warp::post()
        .and(warp::path("dispatch"))
        .and(warp::body::json())
        .and(state_filter.clone())
        .and(warp::any().map(move || dispatch_counter.clone()))
        .and_then(handle_dispatch);

    // POST /route-eta — OSRM distance/duration between two points
    let route_eta = warp::post()
        .and(warp::path("route-eta"))
        .and(warp::body::json())
        .and_then(handle_route_eta);

    // SSE /events
    let sse = warp::path("events")
        .and(warp::get())
        .and(tx_filter.clone())
        .map(|tx: broadcast::Sender<DriverUpdate>| {
            let mut rx = tx.subscribe();
            let stream = async_stream::stream! {
                loop {
                    match rx.recv().await {
                        Ok(update) => {
                            let json = serde_json::to_string(&update).unwrap_or_default();
                            yield Ok::<_, Infallible>(warp::sse::Event::default().data(json));
                        }
                        Err(broadcast::error::RecvError::Lagged(_)) => continue,
                        Err(broadcast::error::RecvError::Closed) => break,
                    }
                }
            };
            warp::sse::reply(warp::sse::keep_alive().stream(stream))
        });

    let metrics_route = warp::path("metrics").map(|| {
        let encoder = TextEncoder::new();
        let metric_families = prometheus::gather();
        let mut buffer = vec![];
        encoder.encode(&metric_families, &mut buffer).unwrap();
        warp::reply::with_header(buffer, "Content-Type", encoder.format_type())
    });

    let routes = track
        .or(track_batch)
        .or(drivers)
        .or(nearby)
        .or(dispatch_route)
        .or(route_eta)
        .or(sse)
        .or(metrics_route)
        .with(warp::cors().allow_any_origin());

    println!("Rust tracker running on http://127.0.0.1:3030");
    println!("Instance shard: index={} count={}", instance_index, instance_count);
    println!("Internal shards: count={} per_shard_capacity={}", shard_count, per_shard_capacity);
    println!("Endpoints: POST /track, POST /track-batch, GET /drivers, GET /nearby, POST /dispatch, POST /route-eta, GET /events, /metrics");
    warp::serve(routes).run(([127,0,0,1], 3030)).await;
}

async fn handle_track(update: DriverUpdate, state: SharedState, tx: broadcast::Sender<DriverUpdate>, instance_info: (usize, usize)) -> Result<impl warp::Reply, Infallible> {
    let (instance_index, instance_count) = instance_info;
    if owns_shard(&update.id, instance_index, instance_count) {
        state.insert(update.clone()).await;
        let _ = tx.send(update.clone());
        Ok(warp::reply::with_status("ok", warp::http::StatusCode::OK))
    } else {
        Ok(warp::reply::with_status("", warp::http::StatusCode::NO_CONTENT))
    }
}

async fn handle_track_batch(updates: Vec<DriverUpdate>, state: SharedState, tx: broadcast::Sender<DriverUpdate>, instance_info: (usize, usize)) -> Result<impl warp::Reply, Infallible> {
    let (instance_index, instance_count) = instance_info;
    let mut last_by_id: HashMap<String, DriverUpdate> = HashMap::with_capacity(updates.len());
    for update in updates.into_iter() {
        if !owns_shard(&update.id, instance_index, instance_count) { continue; }
        last_by_id.insert(update.id.clone(), update);
    }
    let mut per_shard_batches: HashMap<usize, Vec<DriverUpdate>> = HashMap::new();
    for (_, u) in last_by_id {
        let shard_idx = state.shard_for(&u.id);
        per_shard_batches.entry(shard_idx).or_default().push(u);
    }
    for (shard_idx, batch) in per_shard_batches {
        let mut s = state.shards[shard_idx].lock().await;
        for u in batch.iter() {
            s.insert(u.clone());
            let _ = tx.send(u.clone());
        }
    }
    Ok(warp::reply::with_status("ok", warp::http::StatusCode::OK))
}

async fn handle_drivers(state: SharedState) -> Result<impl warp::Reply, Infallible> {
    let drivers = state.all().await;
    let resp = DriversResponse { count: drivers.len(), drivers };
    Ok(warp::reply::json(&resp))
}

async fn handle_nearby(query: std::collections::HashMap<String, String>, state: SharedState) -> Result<impl warp::Reply, Infallible> {
    let lat = query.get("lat").and_then(|v| v.parse::<f64>().ok()).unwrap_or(0.0);
    let lon = query.get("lon").and_then(|v| v.parse::<f64>().ok()).unwrap_or(0.0);
    let radius_deg = query.get("radius_deg").and_then(|v| v.parse::<f64>().ok()).unwrap_or(0.01);
    let radius_m = radius_deg * 111_320.0;
    let mut all = Vec::new();
    for shard in &state.shards {
        let s = shard.lock().await;
        all.extend(s.nearest(lat, lon, radius_m));
    }
    let resp = DriversResponse { count: all.len(), drivers: all.into_iter().map(|r| DriverUpdate {
        id: r.driver_id,
        lat: r.driver_lat,
        lon: r.driver_lon,
        status: None,
        ts_unix_ms: None,
    }).collect() };
    Ok(warp::reply::json(&resp))
}

async fn handle_dispatch(req: OrderDispatchRequest, state: SharedState, counter: CounterVec) -> Result<impl warp::Reply, Infallible> {
    match state.nearest_driver(req.pickup_lat, req.pickup_lon, req.radius_m).await {
        Some(result) => {
            counter.with_label_values(&["success"]).inc();
            Ok(warp::reply::json(&result))
        }
        None => {
            counter.with_label_values(&["no_driver"]).inc();
            Ok(warp::reply::json(&serde_json::json!({"error": "no_nearby_driver", "code": 404})))
        }
    }
}

async fn handle_route_eta(req: RouteETAQuery) -> Result<impl warp::Reply, Infallible> {
    // Use OSRM for accurate driving distance/duration, fallback to haversine
    let url = format!(
        "https://router.project-osrm.org/route/v1/driving/{},{};{},{}?overview=false",
        req.lon1, req.lat1, req.lon2, req.lat2
    );
    match reqwest::get(&url).await {
        Ok(resp) => {
            if let Ok(body) = resp.text().await {
                if let Ok(data) = serde_json::from_str::<serde_json::Value>(&body) {
                    if let Some(route) = data["routes"].get(0) {
                        let dist = route["distance"].as_f64().unwrap_or(0.0);
                        let dur = route["duration"].as_f64().unwrap_or(0.0);
                        return Ok(warp::reply::json(&RouteETAResult {
                            distance_m: dist,
                            duration_sec: dur,
                        }));
                    }
                }
            }
            // Fallback to haversine
            let d = haversine(req.lat1, req.lon1, req.lat2, req.lon2);
            Ok(warp::reply::json(&RouteETAResult {
                distance_m: d,
                duration_sec: d / 8.0,
            }))
        }
        Err(_) => {
            let d = haversine(req.lat1, req.lon1, req.lat2, req.lon2);
            Ok(warp::reply::json(&RouteETAResult {
                distance_m: d,
                duration_sec: d / 8.0,
            }))
        }
    }
}
