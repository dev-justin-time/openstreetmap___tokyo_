"""
Minimal Python worker that polls the Go queue file and performs placeholder spatial analysis.
This is a lightweight scaffold: extend with osmnx or geopandas as needed.
"""
import time
import json
from pathlib import Path

QUEUE = Path("services/go/queue.log")
ANALYSIS_DIR = Path("services/python/analysis")
ANALYSIS_DIR.mkdir(parents=True, exist_ok=True)

def analyze_secondary(entry):
    """
    Secondary, richer analysis using gpxpy + geopandas/osmnx/pandas/shapely/matplotlib.
    Expects `entry` to be a dict with keys:
      - primary: the rust summary dict
      - gpx_raw: string contents of the GPX file
    Produces a 'secondary' dict with richer metrics and saves a plotted route PNG.
    """
    import json
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
    gpx_text = entry.get('gpx_raw', '')
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
    last_size = 0
    while True:
        if QUEUE.exists():
            size = QUEUE.stat().st_size
            if size > last_size:
                with QUEUE.open('r', encoding='utf-8') as f:
                    f.seek(last_size)
                    for line in f:
                        line = line.strip()
                        if not line:
                            continue
                        try:
                            entry = json.loads(line)
                        except Exception:
                            entry = {"raw": line}

                        # If a primary summary exists but no secondary, run heavy analysis
                        primary = entry.get('primary')
                        secondary = entry.get('secondary')
                        if primary is not None and secondary is None:
                            sec = analyze_secondary(entry)
                            # persist secondary alongside a timestamped file for later inspection
                            outfile = ANALYSIS_DIR / f"analysis_{int(time.time())}.json"
                            with outfile.open('w', encoding='utf-8') as out:
                                json.dump({'primary': primary, 'secondary': sec}, out, ensure_ascii=False, indent=2)
                            print("Secondary analysis saved:", outfile)
                        else:
                            print("Skipping entry (no primary or already secondary)", entry.get('primary') is not None)
                last_size = size
        time.sleep(2)

if __name__ == "__main__":
    run()