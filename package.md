  /* @tweakable [Include optional backend packages (Rust/Go/Python) suggestions in the list] */
  INCLUDE_OPTIONAL_BACKENDS = true
  
  # Project dependency inventory
  
  This file lists libraries, services and runtime dependencies currently used by the repository (client and server), plus recommended/optional packages you may want to add for production or feature enhancements.
  
  ## Frontend (browser)
  - leaflet (tiles & map UI)
    - usage: included via importmap -> https://unpkg.com/leaflet@1.9.4/dist/leaflet-src.esm.js
    - purpose: map rendering, markers, polylines.
  - Google Fonts: Noto Sans
    - usage: fonts.googleapis.com
    - purpose: typography only.
  - OSRM public routing API
    - usage: fetch calls to https://router.project-osrm.org
    - purpose: route planning (no package; external HTTP service).
  - Overpass API / Nominatim
    - usage: fetch POST/GET to public Overpass and Nominatim endpoints
    - purpose: POI queries, reverse geocoding, place search (external services).
  - Optional / recommended client libs:
    - nipplejs (mobile joystick control) — recommended for mobile control integration.
    - a small fetch/retry helper (axios or ky) — optional, improves robustness of remote calls.
  
  ## Frontend (local JS modules in repo)
  - config.js, map.js, api.js, simulation.js, ui.js, utils.js, gui.js, betaBanner.js, confirmModal.js, osmIntegration.js
    - usage: internal modular code (no external package manager required).
    - purpose: app logic, simulation, OSM integrations.
  
  ## Driver app (mobile web)
  - leaflet (same as frontend)
  - material icons (Google) — used in driver_app index.html
  - navigator.geolocation (browser API) — used for live location update.
  
  ## Backend: Go (route-engine)
  - Standard library:
    - net/http, encoding/json, math, time, sync, os, context, log, fmt, rand, strconv, strings
    - usage: server implementation (SSE, work queue, persistence).
  - Optional / recommended:
    - gorilla/mux — for more expressive routing (optional).
    - go-redis/redis — if replacing in-memory state with Redis for scale.
    - pgx or gorm — if migrating to PostgreSQL/PostGIS for persistence & geospatial indexing.
    - promhttp / prometheus client — for metrics.
    - cors middleware — if serving APIs across origins.
  
  ## Backend: Rust (GPX parser service)
  - (See services/rust/Cargo.toml) — recommended crates (if not already present):
    - actix-web or warp (HTTP server)
    - serde, serde_json (serialization)
    - gpx or quick-xml (GPX parsing)
    - anyhow / thiserror (error handling)
    - tokio (async runtime) — if using async HTTP server
    - rayon (optional) — parallel processing for heavy GPX jobs
  - Optional:
    - clap (CLI), prometheus (metrics), sqlx (DB)
  
  ## Backend: Python (worker)
  - Recommended packages:
    - gpxpy (GPX parsing)
    - geopandas, shapely (spatial analysis)
    - osmnx (OSM network analysis)
    - pandas (data handling)
    - matplotlib (plots)
    - requests (HTTP helpers)
    - boto3 (if uploading to S3)
  - Runtime: Python 3.8  
  
  ## Dev / Tooling (suggested)
  - Docker & docker-compose — orchestrate Rust, Go, Python services and frontend static server.
  - Node (optional) — for local tooling, bundlers, or to install dev helpers (not required for current plain ES modules).
  - Makefile or simple scripts for local start.
  
  ## Runtime / Operational considerations
  - Reverse geocoding / Overpass: consider proxying or rate-limiting (no package; infra policy).
  - Tile provider: consider alternative tile hosts or API keys for production.
  - Monitoring & logging libraries for Go/Rust/Python as recommended above.
  
  ## How to adjust this list
  Edit the boolean at the top of this file to include or exclude optional backend suggestions:
  /* @tweakable [Include optional backend packages (Rust/Go/Python) suggestions in the list] */
  INCLUDE_OPTIONAL_BACKENDS = true