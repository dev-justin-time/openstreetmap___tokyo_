# KINETIC_FLEET — Polyglot Fleet OS

A full-stack fleet operating system with real-time 1000-driver NYC simulation, CityGML→3D Tiles pipeline, and gamified dispatch demo.

## Architecture

```
Language  Role                               Status
────────  ────────────────────────────────   ──────
Rust      Spatial tracker (R-tree, SSE)      ✅ Release build (0 warnings)
Go        API gateway, WebSocket hub, SSE    ✅ go build + go vet clean
Go        NYC demo server (1000 drivers)     ✅ go build + go vet clean
Go        Driver agent simulator             ✅ go build + go vet clean
Python    Spatial analysis (gpxpy, shapely)  ✅ All imports verified
JS/HTML   8-page frontend suite (MapLibre)   ✅ No structural errors
```

## Services

| Service | Port | Description |
|---------|------|-------------|
| `nyc-app-server` | `:8080` | 1000 simulated NYC drivers, WebSocket `/ws`, REST `/api/assign`, `/api/drivers`, `/api/stats` |
| `bin/gateway.exe` | `:8081` | Go API gateway with WebSocket hub, SSE broker, SQLite queue, order dispatch |
| Rust tracker | `:3030` | In-memory sharded R-tree spatial index, SSE events, `/track`, `/nearby`, `/dispatch` |
| `plateau-server` | `:8080` | CORS-enabled Node.js server for 3D Tiles (`.b3dm`, `.pnts`) |

## Frontend Pages

All 8 pages share a dark HUD theme (Inter + JetBrains Mono, glass panels, Material Symbols) with nav dropdown + settings dropdown:

| Page | Purpose |
|------|---------|
| `onboarding.html` | Feature landing, 3-layer architecture diagram, competitor comparison |
| `command_center.html` | Fleet dashboard with sidebar + top nav |
| `missioncontroll.html` | Mission Control with off-canvas drawer |
| `dispatch.html` | Dispatch panel with sidebar |
| `runtime.html` | Runtime settings with sidebar |
| `driverprofile.html` | Driver profile view |
| `plans.html` | Subscription tiers |
| `login.html` | Login / sign-in |
| `nyc-demo.html` | **NYC Fleet Demo** — MapLibre GL JS, 1000 drivers, game HUD |

## NYC Demo Features

- **MapLibre GL JS** — WebGL-accelerated map with CartoDB dark tiles + color pop overlay
- **Clustering** — `promoteId: 'id'` for O(1) lookups, cluster circles at low zoom
- **Glow markers** — Colored by status (green/yellow/blue) with radial glow layer
- **ThrottledRAF** — 30fps cap decouples WebSocket data rate from render loop
- **WebSocket** — Live 500ms driver stream from `nyc-app-server`
- **Game HUD** — Score, XP/levels (10 ranks), streak, combo multiplier (1–5x), achievements (9 unlockable)
- **Toast notifications** — Combo alerts, achievement unlocks, level-up announcements
- **Keyboard shortcuts** — `A` assign, `R` recenter, `G` toggle game panel

## Game System

| Mechanic | Detail |
|----------|--------|
| XP/Level | 100 + (level-1)×50 per level, 10 titles (Dispatch Cadet → NYC Legend) |
| Combo | Assign within 5s to build 1x→5x multiplier; resets after 10s inactivity |
| Streak | Consecutive successful orders; resets on API failure |
| Score | 100 × combo multiplier per order |
| Achievements | First Dispatch, Delivery Spree (10), Century (100), Speed Demon (3x combo), On Fire (5x), Perfect Streak (20), Precision (50 drivers), Fleet Master (level 10), NYC Legend (500 orders) |

## PLATEAU 3D Tiles Pipeline

```bash
# Convert CityGML → CityJSON → 3D Tiles (memory-efficient)
pip install cjio py3dtiles laspy

cjio input.gml export buildings.city.json
py3dtiles tileset --srs_in EPSG:6697 --srs_out EPSG:4326 --out ./tiles buildings.city.json

# Serve with CORS
cd services/plateau-server && npm start
```

## Quick Start

```bash
# 1. Start NYC demo server
cd services/nyc-app-server && go build -o ../../bin/nyc-app-server.exe .
../../bin/nyc-app-server.exe   # → :8080

# 2. Open frontend
# Open frontend/pages/nyc-demo.html in browser

# 3. (Optional) Start gateway + Rust tracker
cd services/go && go build -o ../../bin/gateway.exe .
cd services/rust && cargo build --release
```

## Build

```bash
# All binaries
cd services/go       && go build -o ../../bin/gateway.exe .
cd services/driver_app && go build -o ../../bin/driver_app.exe .
cd services/nyc-app-server && go build -o ../../bin/nyc-app-server.exe .
cd services/rust     && cargo build --release
cd services/plateau-server && npm install

# Verify
cd services/go && go vet ./...
```

## Tech Stack

- **Go 1.26** — gorilla/websocket, SQLite, SSE
- **Rust 1.96** — rstar R-tree, actix-web, tokio broadcast channels
- **Python 3.14** — gpxpy, geopandas, shapely, py3dtiles, cjio
- **MapLibre GL JS 4.7** — WebGL rendering, clustering, promoteId
- **Node.js** — Express CORS server for 3D Tiles
- **Docker / Caddy** — Reverse proxy, TLS, rate limiting, horizontal scaling
