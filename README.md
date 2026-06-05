# KINETIC_FLEET — Polyglot Fleet OS

A full-stack fleet operating system with real-time 1000-driver NYC simulation, CityGML→3D Tiles pipeline, gamified dispatch demo, and NVIDIA API integration.

## Architecture

```
Language  Role                               Status
────────  ────────────────────────────────   ──────
Rust      Spatial tracker (R-tree, SSE)      ✅ Release build (0 warnings)
Go        API gateway, WebSocket hub, SSE    ✅ go build + go vet clean
Go        NYC demo server (1000 drivers)     ✅ go build + go vet clean
Go        Driver agent simulator             ✅ go build + go vet clean
Python    Spatial analysis (gpxpy, shapely)  ✅ All imports verified
Python    NVIDIA Nemotron client             ✅ openai 2.41.0
JS/HTML   9-page frontend suite (MapLibre)   ✅ No structural errors
Node.js   PLATEAU 3D Tiles server            ✅ npm deps installed
```

## Services

| Service | Port | Description |
|---------|------|-------------|
| `nyc-app-server` | `:8080` | 1000 simulated NYC drivers, WebSocket `/ws`, REST `/api/assign`, `/api/drivers`, `/api/stats`, `/api/nvidia/chat` |
| `bin/gateway.exe` | `:8081` | Go API gateway with WebSocket hub, SSE broker, SQLite queue, order dispatch |
| Rust tracker | `:3030` | In-memory sharded R-tree spatial index, SSE events, `/track`, `/nearby`, `/dispatch` |
| `plateau-server` | `:8080` | CORS-enabled Node.js server for 3D Tiles (`.b3dm`, `.pnts`) |

## Frontend Pages

All 9 pages share a dark HUD theme (Inter + JetBrains Mono, glass panels, Material Symbols) with nav dropdown + settings dropdown:

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
| `nyc-demo.html` | **NYC Fleet Demo** — MapLibre GL JS, 1000 drivers, game HUD, Settings panel, NVIDIA API |

## NYC Demo Features

- **MapLibre GL JS** — WebGL-accelerated map with CartoDB dark tiles + color pop overlay
- **Clustering** — `promoteId: 'id'` for O(1) lookups, cluster circles at low zoom
- **Glow markers** — Colored by status (green/yellow/blue) with radial glow layer
- **ThrottledRAF** — 30fps cap decouples WebSocket data rate from render loop
- **WebSocket** — Live 500ms driver stream from `nyc-app-server`
- **Game HUD** — Score, XP/levels (10 ranks), streak, combo multiplier (1–5x), achievements (9 unlockable)
- **Toast notifications** — Combo alerts, achievement unlocks, level-up announcements
- **NVIDIA API panel** — Interact with NVIDIA Nemotron models directly from the browser
- **Settings panel** — Mode switching, panel visibility, connection status

### Loading States

The nyc-demo page tracks and visualizes several loading states:

| State | Indicator | Location |
|-------|-----------|----------|
| **Map Loading** | Map tiles loading from CartoDB CDN; `settingsMapStatus` shows `LOADING` → `READY` | Settings panel + telemetry |
| **WebSocket Connecting** | Header status dot turns yellow, `CONNECTING` text | Header + settings panel |
| **WebSocket Connected** | Green dot, `CONNECTED` / `LIVE` text | Header + settings + stats panel |
| **WebSocket Disconnected** | Red dot, `DISCONNECTED` / `OFFLINE` text, auto-reconnect every 3s | Header + settings + stats panel |
| **Driver Stream Active** | Driver count updates every 500ms via WebSocket | Stats panel + settings |
| **NVIDIA API Processing** | Pulsing dot animation while waiting for model response | API panel |
| **Order Assignment In Progress** | REST call to `/api/assign`; failure resets streak to 0 | Implicit via game state |

### Demo Mode vs Live Mode

The Settings panel (`S` key or gear icon) provides a mode toggle:

- **Demo Mode** (default) — Connects to `nyc-app-server` on `:8080`, 1000 simulated drivers moving randomly between 17 NYC landmarks. All features (game HUD, assignments, stats) work with simulated data.
- **Live Mode** — Connects to the Go gateway on `:8081`, backed by SQLite queues and the Rust spatial tracker. Expects real driver tracking data.

Mode switching:
1. Closes the current WebSocket connection
2. Updates `WS_URL` and `API_BASE` to the target port
3. Reconnects to the new server
4. Logs the mode change to telemetry

### Settings Panel UI Options

The Settings panel (`S` key or `gear` icon in header) provides:

- **Mode selector** — Demo / Live toggle with info banner
- **Panel visibility** — Checkboxes to show/hide each panel (Fleet Status, Telemetry, Game HUD, Controls, NVIDIA API)
- **Connection status** — Live readout of WebSocket state, driver count, order count, map readiness

## Game System

| Mechanic | Detail |
|----------|--------|
| XP/Level | 100 + (level-1)×50 per level, 10 titles (Dispatch Cadet → NYC Legend) |
| Combo | Assign within 5s to build 1x→5x multiplier; resets after 10s inactivity |
| Streak | Consecutive successful orders; resets on API failure |
| Score | 100 × combo multiplier per order |
| Achievements | First Dispatch, Delivery Spree (10), Century (100), Speed Demon (3x combo), On Fire (5x), Perfect Streak (20), Precision (50 drivers), Fleet Master (level 10), NYC Legend (500 orders) |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `A` | Assign random order to available driver |
| `R` | Recenter map on NYC |
| `G` | Toggle Game HUD panel |
| `N` | Toggle NVIDIA API panel |
| `S` | Toggle Settings panel |

## NVIDIA API Integration

- **Proxy endpoint**: `POST /api/nvidia/chat` on nyc-app-server forwards to `https://integrate.api.nvidia.com/v1/chat/completions`
- **Auth**: `X-NVIDIA-Key` header from frontend, falls back to `NVIDIA_API_KEY` env var
- **Async support**: Handles NVIDIA's 202 + polling pattern automatically
- **Frontend**: Browser-based API panel with key input, model selector (6 models), prompt, temperature, max tokens
- **Reasoning models**: Sends `enable_thinking: true` + `reasoning_budget: 16384` for models with `reasoning` in name
- **Python client**: `services/python/nvidia_nemotron.py` — standalone CLI tool using `openai` package

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

# 4. (Optional) Start plateu 3D tiles server
cd services/plateau-server && npm start  # → :8080 (if nyc-app-server not running)
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
- **Python 3.14** — gpxpy, geopandas, shapely, py3dtiles, cjio, openai
- **MapLibre GL JS 4.7** — WebGL rendering, clustering, promoteId
- **Node.js** — Express CORS server for 3D Tiles
- **Docker / Caddy** — Reverse proxy, TLS, rate limiting, horizontal scaling
