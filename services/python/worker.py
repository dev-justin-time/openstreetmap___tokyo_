"""
Python worker updated to consume the SQLite metadata queue produced by the Go gateway.
The worker will:
 - connect to queue_store/queue.db
 - poll for pending entries
 - mark them 'processing', run analyze_secondary() using the payload_ref (local backup GPX file)
 - save secondary analysis to services/python/analysis and mark queue row 'done'
"""
import time
import json
from pathlib import Path
import sqlite3
import os

QUEUE_DB = Path("queue_store/queue.db")
ANALYSIS_DIR = Path("services/python/analysis")
ANALYSIS_DIR.mkdir(parents=True, exist_ok=True)

def analyze_secondary_from_file(primary_json_str, gpx_path):
    """
    Secondary analysis reading the GPX from file path.
    Returns a dict with analysis results (or error keys).
    """
    try:
        import gpxpy
        import gpxpy.gpx
        import pandas as pd
        from shapely.geometry import Point, LineString
        import geopandas as gpd
        import matplotlib.pyplot as plt
    except Exception as e:
        return {'error': 'missing libraries in environment: ' + str(e)}

    res = {}
    try:
        with open(gpx_path, 'r', encoding='utf-8', errors='ignore') as fh:
            gpx_text = fh.read()
    except Exception as e:
        return {'error': f'failed to read gpx file {gpx_path}: {e}'}

    # parse GPX
    try:
        gpx = gpxpy.parse(gpx_text)
    except Exception as e:
        res['error'] = 'gpxpy parse error: ' + str(e)
        return res

    # collect track points
    pts = []
    for track in gpx.tracks:
        for segment in track.segments:
            for p in segment.points:
                pts.append({
                    'lat': p.latitude,
                    'lon': p.longitude,
                    'ele': p.elevation if p.elevation is not None else 0.0,
                    'time': p.time.isoformat() if p.time else None
                })

    # If trace is long, downsample using Douglas-Peucker to reduce heavy processing cost
    def douglas_peucker(points, epsilon):
        # points: list of dicts with 'lat','lon'
        if not points or len(points) < 3:
            return points
        import math
        def perp_distance(a, b, p):
            # a,b,p are (lat,lon)
            x0, y0 = p
            x1, y1 = a
            x2, y2 = b
            num = abs((y2 - y1)*x0 - (x2 - x1)*y0 + x2*y1 - y2*x1)
            den = math.hypot(y2 - y1, x2 - x1)
            return num / den if den != 0 else math.hypot(x0 - x1, y0 - y1)
        def rdp(pts):
            if len(pts) < 3:
                return pts
            # find point with max distance
            a = (pts[0]['lat'], pts[0]['lon'])
            b = (pts[-1]['lat'], pts[-1]['lon'])
            maxd = 0.0
            idx = 0
            for i in range(1, len(pts)-1):
                d = perp_distance(a, b, (pts[i]['lat'], pts[i]['lon']))
                if d > maxd:
                    idx = i
                    maxd = d
            if maxd > epsilon:
                left = rdp(pts[:idx+1])
                right = rdp(pts[idx:])
                return left[:-1] + right
            else:
                return [pts[0], pts[-1]]
        return rdp(points)

    # choose epsilon dynamically based on point count and bounding box
    if len(pts) > 800:
        # compute bbox diagonal in degrees approx
        lats = [p['lat'] for p in pts]
        lons = [p['lon'] for p in pts]
        lat_span = max(lats) - min(lats) if lats else 0.0
        lon_span = max(lons) - min(lons) if lons else 0.0
        # set epsilon roughly as small fraction of diagonal
        diag = (lat_span**2 + lon_span**2) ** 0.5
        epsilon = max(1e-6, diag * 0.002)  # tunable
        pts = douglas_peucker(pts, epsilon)

    if not pts:
        res['error'] = 'no points found'
        return res

    df = pd.DataFrame(pts)
    # compute simple distances (haversine)
    def haversine(row1, row2):
        import math
        lat1, lon1 = math.radians(row1.lat), math.radians(row1.lon)
        lat2, lon2 = math.radians(row2.lat), math.radians(row2.lon)
        dlat = lat2 - lat1
        dlon = lon2 - lon1
        a = math.sin(dlat/2)**2 + math.cos(lat1)*math.cos(lat2)*(math.sin(dlon/2)**2)
        c = 2 * math.asin(math.sqrt(a))
        return 6371000.0 * c

    distances = []
    for i in range(1, len(df)):
        distances.append(haversine(df.iloc[i-1], df.iloc[i]))
    total_distance = float(sum(distances))

    # elevation gain
    elev_gain = float(sum([max(0, df.iloc[i].ele - df.iloc[i-1].ele) for i in range(1, len(df))]))

    # basic speed stats if time available
    speed_stats = {}
    if 'time' in df.columns and df['time'].notnull().any():
        df['time_obj'] = pd.to_datetime(df['time'])
        total_seconds = (df['time_obj'].iloc[-1] - df['time_obj'].iloc[0]).total_seconds()
        avg_speed_m_s = total_distance / total_seconds if total_seconds > 0 else None
        speed_stats = {
            'total_seconds': total_seconds,
            'avg_m_s': avg_speed_m_s
        }

    # build GeoDataFrame
    gdf = gpd.GeoDataFrame(df, geometry=[Point(xy) for xy in zip(df.lon, df.lat)], crs='EPSG:4326')
    line = LineString([(p.x, p.y) for p in gdf.geometry])
    line_gdf = gpd.GeoDataFrame({'geometry': [line]}, crs='EPSG:4326')

    # Plot route and save
    plot_path = ANALYSIS_DIR / f"route_{int(time.time())}.png"
    try:
        fig, ax = plt.subplots(figsize=(6,6))
        line_gdf.to_crs(epsg=3857).plot(ax=ax, linewidth=3, color='#8b37ff', alpha=0.8)
        gdf.to_crs(epsg=3857).plot(ax=ax, markersize=10, color='#ff375f')
        ax.set_axis_off()
        plt.tight_layout()
        fig.savefig(plot_path, dpi=150)
        plt.close(fig)
    except Exception as e:
        res['plot_error'] = str(e)
        plot_path = None

    res.update({
        'total_distance_m': total_distance,
        'elevation_gain_m': elev_gain,
        'points': len(df),
        'speed_stats': speed_stats,
        'plot_file': str(plot_path) if plot_path else None,
        'note': 'secondary analysis produced by Python worker (gpxpy, geopandas, pandas, shapely, matplotlib)'
    })
    return res

def run():
    if not QUEUE_DB.exists():
        print("queue DB not found at", QUEUE_DB, "waiting for it to appear...")
    # connect with sqlite3 - allow other writers
    while True:
        try:
            conn = sqlite3.connect(str(QUEUE_DB), timeout=5, isolation_level=None)
            break
        except Exception as e:
            print("waiting for queue DB:", e)
            time.sleep(2)

    cur = conn.cursor()
    while True:
        try:
            # fetch a small batch of pending rows
            cur.execute("SELECT id, payload_ref, primary_json FROM queue WHERE status = 'pending' ORDER BY created_at LIMIT 4")
            rows = cur.fetchall()
            if not rows:
                time.sleep(2)
                continue

            for row in rows:
                qid, payload_ref, primary_json = row
                # attempt to atomically mark processing if still pending
                try:
                    cur.execute("UPDATE queue SET status='processing', attempts=attempts+1 WHERE id = ? AND status = 'pending'", (qid,))
                    if cur.rowcount == 0:
                        # someone else picked it
                        continue
                except Exception as e:
                    print("failed to mark processing:", e)
                    continue

                # do secondary analysis using file at payload_ref
                result = analyze_secondary_from_file(primary_json, payload_ref)
                # persist secondary output
                outpath = ANALYSIS_DIR / f"analysis_{int(time.time())}_{qid}.json"
                try:
                    with open(outpath, 'w', encoding='utf-8') as out:
                        json.dump({'primary': json.loads(primary_json) if primary_json else None, 'secondary': result}, out, ensure_ascii=False, indent=2)
                    # mark done
                    cur.execute("UPDATE queue SET status='done' WHERE id = ?", (qid,))
                    print("Saved secondary analysis:", outpath)
                except Exception as e:
                    print("failed to save analysis for", qid, e)
                    # revert to pending so it can be retried later
                    cur.execute("UPDATE queue SET status='pending' WHERE id = ?", (qid,))
            # small pause between batches
            time.sleep(0.5)
        except Exception as e:
            print("worker loop error:", e)
            time.sleep(2)

if __name__ == "__main__":
    run()