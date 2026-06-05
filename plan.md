# Merge & Integration Plan

Goal: Provide a step-by-step plan to merge the frontend, driver app, route-engine, and polyglot services; define integration points, testing checklist, and deployment recommendations. This file contains actions, decisions, and a few @tweakable knobs (JSDoc-style) you can adjust before applying changes.

## High-level overview
- Keep frontend (map    simulation    GUI) and driver_app (mobile-focused driver UI) as two separate builds served by a single static server or via different subpaths (/ and /driver).
- Route-engine (Go server) provides driver pool, order assignment, and SSE for live assignment events.
- Rust service handles GPX parsing; Go gateway proxies uploads to Rust and stores summaries for Python worker processing.
- Python worker processes queue file and writes back enrichments (persisted summaries).
- OSM integrations are client-side; Overpass/Nominatim usage should be proxied or rate-limited in production.

## Merge plan (steps)
1. Repo organization (no files changed yet)
   - Keep current top-level layout. Add an orchestration README and Docker/compose later.
   - Map responsibilities:
     - /driver_app -> driver mobile web client
     - / (root) -> main frontend map simulation
     - /services/go -> route-engine & gateway
     - /services/rust -> GPX parser service
     - /services/python -> post-processing worker

2. Stabilize shared modules
   - Ensure single canonical modules: config.js, map.js, api.js, simulation.js, ui.js.
   - Strategy: create a "shared" folder or keep root modules and import from both builds; during bundle/serve ensure paths resolve.

3. Frontend <-> Simulation integration
   - Define clear runtime APIs:
     - Simulation exports: startAnimation, setFuelLiters, setMoney, setCurrentCountryName, setFollowCar, placeGasStations, get state getters.
     - UI exports: updateHUD, showLoading/hideLoading, updateTurnUI.
   - Confirm dynamic imports are resilient (already used). Add small "shim" files if any circular import errors appear.
   - Tests: manual click-route -> polyline -> animation; HUD updates; country reverse geocode.

4. Driver app integration with route-engine
   - Driver client (driver_app) should POST GPS updates to /driver/{id}/update and open SSE at /driver/{id}/events.
   - Implement a small startup script in driver_app to register driver ID (use localstorage id or random if missing).
   - Test flow:
     - Start Go route-engine
     - Open driver_app, press Connect -> driver appears in /stats
     - POST orders to /order and verify SSE assignment arrives

5. Order generation & assignment
   - Provide a small admin/test CLI or HTTP helper to POST orders to /order (payload includes type, location).
   - Verify assignment sweep matches drivers within radius and SSE messages are delivered.

6. GPX Upload workflow (Rust    Go    Python)
   - Go gateway exposes /upload: accepts multipart form, forwards to Rust parsing API, writes a queue file with raw GPX    Rust summary.
   - Python worker polls queue file and produces richer outputs saved to disk/db; optional: push a callback to Go or store in a known results folder.
   - Tests:
     - Upload sample GPX, check Rust JSON summary returns quickly, Python worker picks up queue item and writes enriched summary.

7. Overpass/Nominatim production concerns
   - Add rate-limiter / proxy for client calls or server-side endpoints to avoid public API throttling.
   - For heavy Overpass queries, run a private Overpass instance or cache results.

8. Map tile reliability fixes
   - Keep the forced refresh flow in map.js; set TILE_LAYER_FORCED_REFRESH_INTERVAL_MS via config or environment in production.
   - Add optional configuration to switch tile providers (for quota/availability).

9. Persistence, DB and scaling (Route-engine)
   - For prototyping: persist drivers to JSON (already provided).
   - For scale: replace with PostgreSQL    PostGIS or a lightweight KV (Redis) for driver positions and job queue.
   - For SSE and assignments: consider sharding by region for 5k drivers; use a worker pool and geospatial index for efficient nearest-driver lookups.

10. CI / Local orchestration
    - Add docker-compose with three services: go (route-engine), rust (gpx), python worker. Frontend served by simple static server (nginx) or local dev server.
    - Health checks: /stats, /health endpoints for services.

## Testing checklist
- [ ] Click-to-route: route line appears and driver animates.
- [ ] HUD updates: speed, fuel, money, ETA, country displayed.
- [ ] Driver SSE: driver receives welcome & assignment events.
- [ ] Order assignment: server assigns order to nearest idle driver within radius.
- [ ] GPX upload: Rust returns summary, Python worker later enriches.
- [ ] Overpass queries return fuel stations and gas markers clickable.
- [ ] Forced tile refresh prevents gray tiles during interactions.

## Deployment checklist
- Proxy Nominatim/Overpass via server to avoid exposing public endpoints.
- Set env config for MAX_DRIVERS, ASSIGN_RADIUS_M, INITIAL_DRIVER_COUNT.
- Use HTTPS & CORS settings for API endpoints and static assets.
- Consider autoscaling route-engine and separating assignment sweeper into worker nodes.

## Security & privacy
- Do not log raw GPS persistently in plain files for production; anonymize or encrypt.
- Use auth for driver endpoints in production (JWT or mTLS).
- Rate-limit upload endpoints.

## Migration notes & potential code edits (developer guidance)
- Fix places where modules attempt to assign to imported names (read-only): use exported setter functions (already added in several places).
- Watch for circular imports: keep UI <-> simulation cross-calls as dynamic imports where possible.
- Consolidate config.js as the single source of tweakable defaults; annotate new tweakables as needed.

## @tweakable knobs (JSDoc style)
The following are tweakable values you can change before applying code edits.

/**
 * @tweakable [maximum number of drivers the route-engine will manage]
 */
const TWEAK_MAX_DRIVERS = 5000;

/**
 * @tweakable [assignment radius in meters used by route-engine matching]
 */
const TWEAK_ASSIGN_RADIUS_M = 5000;

/**
 * @tweakable [initial number of synthetic drivers generated on route-engine startup]
 */
const TWEAK_INITIAL_DRIVER_COUNT = 100;

/**
 * @tweakable [Overpass/Nominatim debounce in ms used by client-side integration]
 */
const TWEAK_NOMINATIM_DEBOUNCE_MS = 600;

/**
 * @tweakable [tile forced refresh interval ms to avoid gray tiles; 0 disables periodic refresh]
 */
const TWEAK_TILE_REFRESH_INTERVAL_MS = 5000;