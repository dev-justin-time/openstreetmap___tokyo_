"""
Operational route visualizer.

Fetches live driver positions and orders from the logistics stack,
renders rich map visualizations using matplotlib + shapely + contextily.

Usage:
  python viz.py                  # single shot, saves to analysis/
  python viz.py --watch 30       # loop every 30s
  python viz.py --serve 9090     # HTTP server on :9090
"""

import argparse
import io
import json
import math
import os
import time
import urllib.request
import urllib.error
from pathlib import Path
from http.server import HTTPServer, BaseHTTPRequestHandler
from typing import Optional

import numpy as np

# ─── Geospatial ──────────────────────────────────────────────────────────────
from shapely.geometry import Point, LineString, MultiPoint
from shapely.ops import unary_union

# ─── Visualization ───────────────────────────────────────────────────────────
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import matplotlib.patches as mpatches

try:
    import cartopy.crs as ccrs
    import cartopy.feature as cfeature
    HAS_CARTOPY = True
except ImportError:
    HAS_CARTOPY = False

try:
    import contextily as ctx
    HAS_CONTEXTILY = True
except ImportError:
    HAS_CONTEXTILY = False

# ─── Config ──────────────────────────────────────────────────────────────────

RUST_API = os.environ.get("RUST_API", "http://127.0.0.1:3030")
GO_API = os.environ.get("GO_API", "http://127.0.0.1:8082")
API_KEY = os.environ.get("API_KEYS", "").split(",")[0] if os.environ.get("API_KEYS") else ""
OUTPUT_DIR = Path(os.environ.get("VIZ_OUTPUT_DIR", "services/python/analysis"))
OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

# ─── Data fetching ───────────────────────────────────────────────────────────

def _fetch(url: str) -> Optional[dict]:
    req = urllib.request.Request(url)
    if API_KEY:
        req.add_header("X-API-Key", API_KEY)
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            return json.loads(resp.read().decode())
    except (urllib.error.URLError, json.JSONDecodeError, OSError) as e:
        print(f"  [warn] fetch failed {url}: {e}")
        return None


def fetch_drivers() -> list:
    """Get live driver positions from Rust tracker."""
    data = _fetch(f"{RUST_API}/drivers")
    if data:
        return data.get("drivers", [])
    return []


def fetch_orders(status: str = "") -> list:
    """Get orders from Go logistics API."""
    url = f"{GO_API}/api/orders"
    if status:
        url += f"?status={status}"
    data = _fetch(url)
    if data:
        return data.get("orders", [])
    return []


def fetch_stats() -> dict:
    """Get operational stats."""
    data = _fetch(f"{GO_API}/api/stats")
    return data or {}


# ─── Geometry helpers ────────────────────────────────────────────────────────

def haversine(lat1: float, lon1: float, lat2: float, lon2: float) -> float:
    """Great-circle distance in meters."""
    R = 6371000.0
    dlat = math.radians(lat2 - lat1)
    dlon = math.radians(lon2 - lon1)
    a = (math.sin(dlat / 2) ** 2 +
         math.cos(math.radians(lat1)) * math.cos(math.radians(lat2)) *
         math.sin(dlon / 2) ** 2)
    return R * 2 * math.atan2(math.sqrt(a), math.sqrt(1 - a))


def interpolate_route(lat1: float, lon1: float, lat2: float, lon2: float,
                      n_points: int = 100) -> np.ndarray:
    """Simple great-circle interpolation between two points."""
    t = np.linspace(0, 1, n_points)
    lats = lat1 + (lat2 - lat1) * t
    lons = lon1 + (lon2 - lon1) * t
    return np.column_stack([lons, lats])


def compute_bounds(drivers: list, orders: list, padding_deg: float = 0.02) -> tuple:
    """
    Compute bounding box from driver + order positions.
    Returns (min_lon, min_lat, max_lon, max_lat).
    """
    all_lats = []
    all_lons = []
    for d in drivers:
        all_lats.append(d.get("lat", 0))
        all_lons.append(d.get("lon", 0))
    for o in orders:
        all_lats.append(o.get("pickup_lat", 0))
        all_lons.append(o.get("pickup_lon", 0))
        all_lats.append(o.get("dropoff_lat", 0))
        all_lons.append(o.get("dropoff_lon", 0))
    if not all_lats:
        return (-0.1, -0.1, 0.1, 0.1)
    return (
        min(all_lons) - padding_deg,
        min(all_lats) - padding_deg,
        max(all_lons) + padding_deg,
        max(all_lats) + padding_deg,
    )


# ─── Visualization ───────────────────────────────────────────────────────────

def render_map(drivers: list, orders: list, stats: dict,
               title: str = "Logistics Operations",
               filename: str = "ops_map.png") -> Optional[Path]:
    """
    Create a rich operational map with:
      - contextily or cartopy basemap tiles
      - driver positions (colored by availability)
      - order pickup/dropoff markers with route lines
      - legend and stats inset
    """
    if not drivers and not orders:
        print("  [skip] no data to render")
        return None

    min_lon, min_lat, max_lon, max_lat = compute_bounds(drivers, orders, 0.03)

    fig, ax = plt.subplots(figsize=(16, 12), dpi=150)

    # ── Basemap ──────────────────────────────────────────────────────────
    if HAS_CARTOPY:
        # Cartopy with OSM tiles
        ax = fig.add_subplot(1, 1, 1, projection=ccrs.PlateCarree())
        ax.set_extent([min_lon - 0.01, max_lon + 0.01,
                       min_lat - 0.01, max_lat + 0.01],
                      crs=ccrs.PlateCarree())
        ax.add_feature(cfeature.COASTLINE, linewidth=0.5, alpha=0.6)
        ax.add_feature(cfeature.BORDERS, linewidth=0.3, alpha=0.4)
        ax.add_feature(cfeature.OCEAN, alpha=0.1)
        ax.add_feature(cfeature.LAND, alpha=0.05)
        ax.add_feature(cfeature.LAKES, alpha=0.1)
        transform = ccrs.PlateCarree()

        if HAS_CONTEXTILY:
            try:
                ctx.add_basemap(ax, source=ctx.providers.OpenStreetMap.Mapnik,
                                crs="EPSG:4326", alpha=0.7)
            except Exception:
                pass
    else:
        # plain matplotlib with optional contextily
        ax.set_xlim(min_lon - 0.01, max_lon + 0.01)
        ax.set_ylim(min_lat - 0.01, max_lat + 0.01)
        transform = None

        if HAS_CONTEXTILY:
            try:
                ctx.add_basemap(ax, source=ctx.providers.OpenStreetMap.Mapnik,
                                crs="EPSG:4326", alpha=0.7)
            except Exception:
                ax.set_facecolor("#f0f0f0")

    ax.set_title(title, fontsize=16, fontweight="bold", pad=16)

    # ── Render drivers ───────────────────────────────────────────────────
    avail_lons, avail_lats = [], []
    busy_lons, busy_lats = [], []
    for d in drivers:
        status = d.get("status", "available")
        if status == "available":
            avail_lons.append(d["lon"])
            avail_lats.append(d["lat"])
        else:
            busy_lons.append(d["lon"])
            busy_lats.append(d["lat"])

    if avail_lats:
        ax.scatter(avail_lons, avail_lats, c="#00ff88", s=18,
                   edgecolors="#111", linewidth=0.3, alpha=0.85,
                   label=f"Available ({len(avail_lats)})", transform=transform,
                   zorder=5)
    if busy_lats:
        ax.scatter(busy_lons, busy_lats, c="#ff5252", s=14,
                   edgecolors="#111", linewidth=0.3, alpha=0.7,
                   label=f"Busy ({len(busy_lats)})", transform=transform,
                   zorder=5)

    # ── Render orders ────────────────────────────────────────────────────
    order_colors = {
        "pending": "#ffb700",
        "assigned": "#00d4ff",
        "picked_up": "#8888ff",
        "delivered": "#00ff88",
        "cancelled": "#ff5252",
    }

    for o in orders:
        color = order_colors.get(o.get("status", "pending"), "#888")
        p_lat, p_lon = o.get("pickup_lat", 0), o.get("pickup_lon", 0)
        d_lat, d_lon = o.get("dropoff_lat", 0), o.get("dropoff_lon", 0)

        # Pickup marker
        ax.scatter(p_lon, p_lat, c=color, s=80, marker="o",
                   edgecolors="white", linewidth=0.5, transform=transform,
                   zorder=6, alpha=0.9)
        ax.annotate("P", (p_lon, p_lat), fontsize=6, ha="center", va="center",
                    color="white", fontweight="bold", transform=transform,
                    zorder=7)

        # Dropoff marker
        if d_lat and d_lon:
            ax.scatter(d_lon, d_lat, c=color, s=60, marker="s",
                       edgecolors="white", linewidth=0.5, transform=transform,
                       zorder=6, alpha=0.9)
            ax.annotate("D", (d_lon, d_lat), fontsize=6, ha="center",
                        va="center", color="white", fontweight="bold",
                        transform=transform, zorder=7)

            # Route line
            route = interpolate_route(p_lat, p_lon, d_lat, d_lon)
            dist = haversine(p_lat, p_lon, d_lat, d_lon)
            ax.plot(route[:, 0], route[:, 1], color=color, linewidth=1.2,
                    linestyle="--", alpha=0.5, transform=transform, zorder=3)

    # ── Stats inset ──────────────────────────────────────────────────────
    stats_text = (
        f"Drivers: {len(drivers)}  |  "
        f"Pending: {stats.get('orders_pending', 0)}  |  "
        f"Assigned: {stats.get('orders_assigned', 0)}  |  "
        f"Delivered: {stats.get('orders_delivered', 0)}  |  "
        f"Total: {stats.get('orders_total', 0)}"
    )
    ax.text(0.5, 0.98, stats_text, transform=ax.transAxes, fontsize=9,
            ha="center", va="top", bbox=dict(boxstyle="round,pad=0.4",
                                            facecolor="white", alpha=0.85,
                                            edgecolor="#ccc"))

    # ── Legend ───────────────────────────────────────────────────────────
    legend_elements = [
        mpatches.Patch(color="#00ff88", label=f"Available ({len(avail_lats)})"),
        mpatches.Patch(color="#ff5252", label=f"Busy ({len(busy_lats)})"),
        plt.Line2D([0], [0], marker="o", color="w", label="Pickup",
                   markerfacecolor="#ffb700", markersize=8),
        plt.Line2D([0], [0], marker="s", color="w", label="Dropoff",
                   markerfacecolor="#ffb700", markersize=7),
        plt.Line2D([0], [0], linestyle="--", color="#888", label="Route",
                   linewidth=2),
    ]
    if HAS_CONTEXTILY or HAS_CARTOPY:
        legend_loc = "lower right"
    else:
        legend_loc = "upper right"
    ax.legend(handles=legend_elements, loc=legend_loc, fontsize=8,
              framealpha=0.85, edgecolor="#ccc")

    ax.set_xlabel("Longitude", fontsize=10)
    ax.set_ylabel("Latitude", fontsize=10)
    ax.grid(True, alpha=0.15, linewidth=0.3)

    plt.tight_layout()

    out_path = OUTPUT_DIR / filename
    fig.savefig(out_path, dpi=150, bbox_inches="tight",
                facecolor=fig.get_facecolor())
    plt.close(fig)
    print(f"  [ok] saved {out_path} ({out_path.stat().st_size / 1024:.0f} KB)")
    return out_path


# ─── Heatmap layer ───────────────────────────────────────────────────────────

def render_heatmap(drivers: list, orders: list,
                   title: str = "Driver Density Heatmap",
                   filename: str = "heatmap.png") -> Optional[Path]:
    """
    Kernel density heatmap of driver positions with order overlay.
    """
    if not drivers:
        print("  [skip] no drivers for heatmap")
        return None

    min_lon, min_lat, max_lon, max_lat = compute_bounds(drivers, orders, 0.04)
    lats = np.array([d["lat"] for d in drivers])
    lons = np.array([d["lon"] for d in drivers])

    fig, ax = plt.subplots(figsize=(14, 10), dpi=120)

    if HAS_CONTEXTILY:
        try:
            ctx.add_basemap(ax, source=ctx.providers.CartoDB.DarkMatter,
                            crs="EPSG:4326", alpha=0.9)
        except Exception:
            ax.set_facecolor("#111")
    else:
        ax.set_facecolor("#111")

    ax.set_xlim(min_lon - 0.01, max_lon + 0.01)
    ax.set_ylim(min_lat - 0.01, max_lat + 0.01)

    # 2D histogram / hexbin
    hb = ax.hexbin(lons, lats, gridsize=40, cmap="plasma",
                   mincnt=1, alpha=0.75, zorder=3)
    cb = fig.colorbar(hb, ax=ax, shrink=0.6, pad=0.02)
    cb.set_label("Driver density", fontsize=9)

    # Order overlays
    order_colors = {"pending": "#ffb700", "assigned": "#00d4ff",
                    "delivered": "#00ff88"}
    for o in orders:
        color = order_colors.get(o.get("status", "pending"), "#888")
        ax.scatter(o.get("pickup_lon"), o.get("pickup_lat"),
                   c=color, s=50, marker="o", edgecolors="white",
                   linewidth=0.5, zorder=5)
        if o.get("dropoff_lon"):
            ax.scatter(o.get("dropoff_lon"), o.get("dropoff_lat"),
                       c=color, s=40, marker="s", edgecolors="white",
                       linewidth=0.5, zorder=5)

    ax.set_title(title, fontsize=14, fontweight="bold", color="white", pad=12)
    ax.set_xlabel("Longitude", fontsize=9, color="#ccc")
    ax.set_ylabel("Latitude", fontsize=9, color="#ccc")
    ax.tick_params(colors="#ccc")

    plt.tight_layout()
    out_path = OUTPUT_DIR / filename
    fig.savefig(out_path, dpi=120, bbox_inches="tight",
                facecolor="#111")
    plt.close(fig)
    print(f"  [ok] saved {out_path} ({out_path.stat().st_size / 1024:.0f} KB)")
    return out_path


# ─── Run loop ────────────────────────────────────────────────────────────────

def run_once() -> dict:
    """Fetch data and render all visualizations. Returns stats dict."""
    print("[viz] fetching data...")
    drivers = fetch_drivers()
    orders = fetch_orders()
    stats = fetch_stats()
    print(f"       drivers={len(drivers)} orders={len(orders)}")

    ts = int(time.time())
    render_map(drivers, orders, stats,
               title=f"Logistics Operations — {time.strftime('%Y-%m-%d %H:%M:%S')}",
               filename=f"ops_map_{ts}.png")
    render_heatmap(drivers, orders,
                   title=f"Driver Density — {time.strftime('%Y-%m-%d %H:%M:%S')}",
                   filename=f"heatmap_{ts}.png")

    # Always write latest
    render_map(drivers, orders, stats,
               title=f"Logistics Operations (live)",
               filename="ops_map_latest.png")
    render_heatmap(drivers, orders,
                   title="Driver Density (live)",
                   filename="heatmap_latest.png")

    return stats


def watch_loop(interval_sec: int = 30):
    """Continuously generate visualizations every interval_sec."""
    print(f"[viz] watching every {interval_sec}s (Ctrl+C to stop)...")
    while True:
        try:
            run_once()
        except KeyboardInterrupt:
            break
        except Exception as e:
            print(f"  [error] {e}")
        time.sleep(interval_sec)


# ─── HTTP server ─────────────────────────────────────────────────────────────

class VizHandler(BaseHTTPRequestHandler):
    """Serves the latest visualization images."""

    def do_GET(self):
        if self.path == "/" or self.path == "/index.html":
            self._serve_html()
        elif self.path == "/ops_map.png":
            self._serve_file("ops_map_latest.png", "image/png")
        elif self.path == "/heatmap.png":
            self._serve_file("heatmap_latest.png", "image/png")
        elif self.path == "/data":
            self._serve_json()
        elif self.path == "/refresh":
            try:
                run_once()
                self._serve_json({"status": "ok"})
            except Exception as e:
                self._serve_json({"status": "error", "message": str(e)}, 500)
        else:
            self.send_error(404)

    def _serve_html(self):
        html = """<!DOCTYPE html>
<html><head><meta charset="utf-8">
<title>Logistics Viz</title>
<style>
body { font-family:system-ui,sans-serif; background:#111; color:#e0e0e0; margin:0; padding:20px; }
h1 { color:#00d4ff; }
img { max-width:100%; border-radius:8px; border:1px solid #333; margin:10px 0; }
.grid { display:grid; grid-template-columns:1fr 1fr; gap:16px; }
@media(max-width:800px){ .grid{grid-template-columns:1fr; } }
button { background:#00d4ff; color:#111; border:none; padding:8px 16px; border-radius:6px; cursor:pointer; font-weight:600; }
button:hover { opacity:0.85; }
#stats { background:#1a1a2e; padding:12px; border-radius:8px; margin:10px 0; font-size:14px; }
</style></head><body>
<h1>Logistics Operations Dashboard</h1>
<button onclick="refresh()">↻ Refresh</button>
<div id="stats">Loading...</div>
<div class="grid">
<div><h3>Operations Map</h3><img src="/ops_map.png" id="map1"></div>
<div><h3>Driver Density</h3><img src="/heatmap.png" id="map2"></div>
</div>
<script>
async function refresh() {
  const btn = document.querySelector('button');
  btn.textContent = '↻ Refreshing...';
  await fetch('/refresh');
  document.getElementById('map1').src = '/ops_map.png?' + Date.now();
  document.getElementById('map2').src = '/heatmap.png?' + Date.now();
  fetch('/data').then(r=>r.json()).then(d=>{
    document.getElementById('stats').innerHTML =
      `Drivers: ${d.drivers} | Orders: ${d.orders_total} | Pending: ${d.orders_pending} | Delivered: ${d.orders_delivered}`;
  });
  btn.textContent = '↻ Refresh';
}
refresh();
setInterval(refresh, 30000);
</script></body></html>"""
        self.send_response(200)
        self.send_header("Content-Type", "text/html")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        self.wfile.write(html.encode())

    def _serve_file(self, name: str, mime: str):
        path = OUTPUT_DIR / name
        if not path.exists():
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Type", mime)
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        with open(path, "rb") as f:
            self.wfile.write(f.read())

    def _serve_json(self, data=None, status: int = 200):
        if data is None:
            drivers = fetch_drivers()
            orders = fetch_orders()
            stats = fetch_stats() if not orders else {}
            data = {
                "drivers": len(drivers),
                "orders_total": len(orders),
                "orders_pending": sum(1 for o in orders if o.get("status") == "pending"),
                "orders_delivered": sum(1 for o in orders if o.get("status") == "delivered"),
                "orders_assigned": sum(1 for o in orders if o.get("status") == "assigned"),
            }
        body = json.dumps(data).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        pass  # quieter


def serve_forever(port: int = 9090):
    """Start HTTP server that serves visualization images."""
    addr = ("0.0.0.0", port)
    httpd = HTTPServer(addr, VizHandler)

    # Generate initial images
    print("[viz] generating initial maps...")
    try:
        run_once()
    except Exception as e:
        print(f"  [warn] initial render: {e}")

    print(f"[viz] HTTP server on http://0.0.0.0:{port}")
    print(f"      /           — dashboard HTML")
    print(f"      /ops_map.png — latest operations map")
    print(f"      /heatmap.png — latest heatmap")
    print(f"      /data        — JSON stats")
    print(f"      /refresh     — regenerate + JSON ok")
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        print("\n[viz] stopping")


# ─── Main ────────────────────────────────────────────────────────────────────

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Logistics route visualizer")
    parser.add_argument("--watch", type=int, default=0,
                        help="Watch mode: regenerate every N seconds")
    parser.add_argument("--serve", type=int, default=0,
                        help="Serve mode: HTTP port (e.g. 9090)")
    parser.add_argument("--once", action="store_true", default=True,
                        help="Run once and exit (default)")
    args = parser.parse_args()

    if args.serve:
        serve_forever(args.serve)
    elif args.watch:
        watch_loop(args.watch)
    else:
        run_once()
