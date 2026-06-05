package main

import (
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ─── GPX XML schema ─────────────────────────────────────────────────────────

type GPX struct {
	XMLName  xml.Name  `xml:"gpx"`
	Version  string    `xml:"version,attr"`
	Creator  string    `xml:"creator,attr"`
	Metadata *Metadata `xml:"metadata"`
	Wpt      []Wpt     `xml:"wpt"`
	Rte      []Rte     `xml:"rte"`
	Trk      []Trk     `xml:"trk"`
}

type Metadata struct {
	Name string `xml:"name"`
	Desc string `xml:"desc"`
	Time string `xml:"time"`
}

type Wpt struct {
	Lat  float64 `xml:"lat,attr"`
	Lon  float64 `xml:"lon,attr"`
	Ele  float64 `xml:"ele"`
	Name string  `xml:"name"`
	Desc string  `xml:"desc"`
	Time string  `xml:"time"`
}

type Rte struct {
	Name   string  `xml:"name"`
	Desc   string  `xml:"desc"`
	Rtept  []Wpt   `xml:"rtept"`
}

type Trk struct {
	Name   string   `xml:"name"`
	Desc   string   `xml:"desc"`
	Trkseg []Trkseg `xml:"trkseg"`
}

type Trkseg struct {
	Trkpt []Wpt `xml:"trkpt"`
}

// ─── Database ───────────────────────────────────────────────────────────────

var gpxDB *sql.DB

func openGPXStore() error {
	if err := os.MkdirAll("gpx_store", 0o755); err != nil {
		return err
	}
	dbPath := filepath.Join("gpx_store", "gpx.db")
	var err error
	gpxDB, err = sql.Open("sqlite3", dbPath+"?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=ON")
	if err != nil {
		return err
	}
	schema := `
	CREATE TABLE IF NOT EXISTS tracks (
	  id INTEGER PRIMARY KEY AUTOINCREMENT,
	  name TEXT,
	  description TEXT,
	  total_distance_m REAL DEFAULT 0,
	  total_elevation_gain_m REAL DEFAULT 0,
	  point_count INTEGER DEFAULT 0,
	  avg_speed_m_s REAL DEFAULT 0,
	  duration_seconds REAL DEFAULT 0,
	  bbox_min_lat REAL,
	  bbox_max_lat REAL,
	  bbox_min_lon REAL,
	  bbox_max_lon REAL,
	  source_file TEXT,
	  created_at TEXT NOT NULL DEFAULT (datetime('now')),
	  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS track_points (
	  id INTEGER PRIMARY KEY AUTOINCREMENT,
	  track_id INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
	  lat REAL NOT NULL,
	  lon REAL NOT NULL,
	  elevation REAL DEFAULT 0,
	  time TEXT,
	  point_index INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_track_points_track ON track_points(track_id, point_index);

	CREATE TABLE IF NOT EXISTS waypoints (
	  id INTEGER PRIMARY KEY AUTOINCREMENT,
	  lat REAL NOT NULL,
	  lon REAL NOT NULL,
	  elevation REAL DEFAULT 0,
	  name TEXT,
	  description TEXT,
	  time TEXT,
	  created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS gpx_routes (
	  id INTEGER PRIMARY KEY AUTOINCREMENT,
	  name TEXT,
	  description TEXT,
	  created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS route_points (
	  id INTEGER PRIMARY KEY AUTOINCREMENT,
	  route_id INTEGER NOT NULL REFERENCES gpx_routes(id) ON DELETE CASCADE,
	  lat REAL NOT NULL,
	  lon REAL NOT NULL,
	  elevation REAL DEFAULT 0,
	  point_index INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_route_points_route ON route_points(route_id, point_index);
	`
	_, err = gpxDB.Exec(schema)
	return err
}

// ─── GPX parsing ────────────────────────────────────────────────────────────

func parseGPX(data []byte) (*GPX, error) {
	var gpx GPX
	if err := xml.Unmarshal(data, &gpx); err != nil {
		return nil, fmt.Errorf("xml parse error: %w", err)
	}
	if gpx.Version == "" {
		gpx.Version = "1.1"
	}
	return &gpx, nil
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return 6371.0 * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func storeTrack(gpx *GPX, sourceFile string) (int64, error) {
	for _, trk := range gpx.Trk {
		var allPts []Wpt
		for _, seg := range trk.Trkseg {
			allPts = append(allPts, seg.Trkpt...)
		}
		if len(allPts) == 0 {
			continue
		}

		// Compute stats
		var totalDist, totalElev float64
		minLat, maxLat := allPts[0].Lat, allPts[0].Lat
		minLon, maxLon := allPts[0].Lon, allPts[0].Lon
		for i := 1; i < len(allPts); i++ {
			totalDist += haversineKm(allPts[i-1].Lat, allPts[i-1].Lon, allPts[i].Lat, allPts[i].Lon)
			if allPts[i].Ele > allPts[i-1].Ele {
				totalElev += allPts[i].Ele - allPts[i-1].Ele
			}
			if allPts[i].Lat < minLat { minLat = allPts[i].Lat }
			if allPts[i].Lat > maxLat { maxLat = allPts[i].Lat }
			if allPts[i].Lon < minLon { minLon = allPts[i].Lon }
			if allPts[i].Lon > maxLon { maxLon = allPts[i].Lon }
		}
		totalDistKm := totalDist
		totalDistM := totalDistKm * 1000

		// Duration
		var durSec float64
		if allPts[0].Time != "" && allPts[len(allPts)-1].Time != "" {
			t0, err0 := time.Parse(time.RFC3339, allPts[0].Time)
			t1, err1 := time.Parse(time.RFC3339, allPts[len(allPts)-1].Time)
			if err0 == nil && err1 == nil && t1.After(t0) {
				durSec = t1.Sub(t0).Seconds()
			}
		}
		avgSpeed := 0.0
		if durSec > 0 {
			avgSpeed = totalDistM / durSec
		}

		name := trk.Name
		if name == "" && gpx.Metadata != nil {
			name = gpx.Metadata.Name
		}

		// Insert track record
		res, err := gpxDB.Exec(`
			INSERT INTO tracks (name, description, total_distance_m, total_elevation_gain_m,
				point_count, avg_speed_m_s, duration_seconds,
				bbox_min_lat, bbox_max_lat, bbox_min_lon, bbox_max_lon, source_file)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			name, trk.Desc, totalDistM, totalElev, len(allPts),
			avgSpeed, durSec, minLat, maxLat, minLon, maxLon, sourceFile)
		if err != nil {
			return 0, err
		}
		trackID, _ := res.LastInsertId()

		// Insert points in batch
		tx, err := gpxDB.Begin()
		if err != nil {
			return 0, err
		}
		stmt, err := tx.Prepare(`INSERT INTO track_points (track_id, lat, lon, elevation, time, point_index) VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			tx.Rollback()
			return 0, err
		}
		defer stmt.Close()

		for i, p := range allPts {
			timeStr := p.Time
			if timeStr != "" {
				if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
					timeStr = t.UTC().Format(time.RFC3339)
				}
			}
			if _, err := stmt.Exec(trackID, p.Lat, p.Lon, p.Ele, timeStr, i); err != nil {
				tx.Rollback()
				return 0, err
			}
		}
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return trackID, nil
	}
	return 0, fmt.Errorf("no tracks found in GPX")
}

func storeWaypoints(gpx *GPX) (int, error) {
	if len(gpx.Wpt) == 0 {
		return 0, nil
	}
	tx, err := gpxDB.Begin()
	if err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(`INSERT INTO waypoints (lat, lon, elevation, name, description, time) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	count := 0
	for _, w := range gpx.Wpt {
		if _, err := stmt.Exec(w.Lat, w.Lon, w.Ele, w.Name, w.Desc, w.Time); err != nil {
			tx.Rollback()
			return 0, err
		}
		count++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func storeRoutes(gpx *GPX) (int, error) {
	if len(gpx.Rte) == 0 {
		return 0, nil
	}
	count := 0
	for _, rte := range gpx.Rte {
		if len(rte.Rtept) == 0 {
			continue
		}
		res, err := gpxDB.Exec(`INSERT INTO gpx_routes (name, description) VALUES (?, ?)`, rte.Name, rte.Desc)
		if err != nil {
			return 0, err
		}
		routeID, _ := res.LastInsertId()
		tx, err := gpxDB.Begin()
		if err != nil {
			return 0, err
		}
		stmt, err := tx.Prepare(`INSERT INTO route_points (route_id, lat, lon, elevation, point_index) VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			tx.Rollback()
			return 0, err
		}
		for i, p := range rte.Rtept {
			if _, err := stmt.Exec(routeID, p.Lat, p.Lon, p.Ele, i); err != nil {
				tx.Rollback()
				return 0, err
			}
		}
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}

// ─── GPX export ─────────────────────────────────────────────────────────────

func exportTrackToGPX(trackID int64) ([]byte, error) {
	var name, desc string
	err := gpxDB.QueryRow(`SELECT name, description FROM tracks WHERE id = ?`, trackID).Scan(&name, &desc)
	if err != nil {
		return nil, fmt.Errorf("track not found: %w", err)
	}

	rows, err := gpxDB.Query(
		`SELECT lat, lon, elevation, COALESCE(time,'') FROM track_points WHERE track_id = ? ORDER BY point_index`, trackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pts := make([]Wpt, 0)
	for rows.Next() {
		var p Wpt
		if err := rows.Scan(&p.Lat, &p.Lon, &p.Ele, &p.Time); err != nil {
			return nil, err
		}
		pts = append(pts, p)
	}
	if len(pts) == 0 {
		return nil, fmt.Errorf("no points in track %d", trackID)
	}

	gpx := GPX{
		Version: "1.1",
		Creator: "osm-gateway gpx_store",
		Metadata: &Metadata{
			Name: name,
			Desc: desc,
			Time: time.Now().UTC().Format(time.RFC3339),
		},
		Trk: []Trk{{
			Name: name,
			Desc: desc,
			Trkseg: []Trkseg{{Trkpt: pts}},
		}},
	}

	out, err := xml.MarshalIndent(gpx, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}

// ─── HTTP handlers ──────────────────────────────────────────────────────────

type GPXStoreResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

func gpxJSON(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func gpxError(w http.ResponseWriter, msg string, status int) {
	gpxJSON(w, GPXStoreResponse{Status: "error", Error: msg}, status)
}

func handleGPXUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		gpxError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		gpxError(w, "read error: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !bytesContainsLower(data, "<gpx") {
		gpxError(w, "not a valid GPX file (missing <gpx>)", http.StatusBadRequest)
		return
	}

	gpx, err := parseGPX(data)
	if err != nil {
		gpxError(w, "parse error: "+err.Error(), http.StatusBadRequest)
		return
	}

	sourceFile := r.URL.Query().Get("filename")
	if sourceFile == "" {
		sourceFile = fmt.Sprintf("upload_%d.gpx", time.Now().Unix())
	}

	trackID, tErr := storeTrack(gpx, sourceFile)
	wptCount, _ := storeWaypoints(gpx)
	rteCount, _ := storeRoutes(gpx)

	meta := map[string]interface{}{
		"source_file":     sourceFile,
		"version":         gpx.Version,
		"creator":         gpx.Creator,
		"track_count":     0,
		"waypoint_count":  wptCount,
		"route_count":     rteCount,
	}
	if tErr == nil {
		meta["track_count"] = 1
		meta["track_id"] = trackID
		// update updated_at
		gpxDB.Exec(`UPDATE tracks SET updated_at = datetime('now') WHERE id = ?`, trackID)
	}

	gpxJSON(w, GPXStoreResponse{Status: "ok", Data: meta}, http.StatusOK)
}

func handleGPXListTracks(w http.ResponseWriter, r *http.Request) {
	rows, err := gpxDB.Query(`
		SELECT id, COALESCE(name,''), COALESCE(description,''), total_distance_m,
			total_elevation_gain_m, point_count, avg_speed_m_s, duration_seconds,
			bbox_min_lat, bbox_max_lat, bbox_min_lon, bbox_max_lon,
			COALESCE(source_file,''), created_at, updated_at
		FROM tracks ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		gpxError(w, "query error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	tracks := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var name, desc, srcFile, created, updated string
		var dist, elev, avgSpeed, dur, minLat, maxLat, minLon, maxLon float64
		var pts int
		if err := rows.Scan(&id, &name, &desc, &dist, &elev, &pts, &avgSpeed, &dur,
			&minLat, &maxLat, &minLon, &maxLon, &srcFile, &created, &updated); err != nil {
			continue
		}
		tracks = append(tracks, map[string]interface{}{
			"id":                   id,
			"name":                 name,
			"description":          desc,
			"total_distance_m":     dist,
			"total_elevation_gain_m": elev,
			"point_count":          pts,
			"avg_speed_m_s":        avgSpeed,
			"duration_seconds":     dur,
			"bbox":                 []float64{minLat, maxLat, minLon, maxLon},
			"source_file":          srcFile,
			"created_at":           created,
			"updated_at":           updated,
		})
	}
	gpxJSON(w, GPXStoreResponse{Status: "ok", Data: tracks}, http.StatusOK)
}

func handleGPXGetTrack(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/gpx/tracks/")
	if strings.Contains(idStr, "/") {
		idStr = strings.Split(idStr, "/")[0]
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		gpxError(w, "invalid track id", http.StatusBadRequest)
		return
	}

	// If export requested
	if strings.HasSuffix(r.URL.Path, "/export") {
		data, err := exportTrackToGPX(id)
		if err != nil {
			gpxError(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/gpx+xml")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="track_%d.gpx"`, id))
		w.WriteHeader(http.StatusOK)
		w.Write(data)
		return
	}

	var name, desc, srcFile, created, updated string
	var dist, elev, avgSpeed, dur, minLat, maxLat, minLon, maxLon float64
	var pts int
	err = gpxDB.QueryRow(`
		SELECT COALESCE(name,''), COALESCE(description,''), total_distance_m,
			total_elevation_gain_m, point_count, avg_speed_m_s, duration_seconds,
			bbox_min_lat, bbox_max_lat, bbox_min_lon, bbox_max_lon,
			COALESCE(source_file,''), created_at, updated_at
		FROM tracks WHERE id = ?`, id).Scan(
		&name, &desc, &dist, &elev, &pts, &avgSpeed, &dur,
		&minLat, &maxLat, &minLon, &maxLon, &srcFile, &created, &updated)
	if err != nil {
		gpxError(w, "track not found", http.StatusNotFound)
		return
	}

	track := map[string]interface{}{
		"id":                   id,
		"name":                 name,
		"description":          desc,
		"total_distance_m":     dist,
		"total_elevation_gain_m": elev,
		"point_count":          pts,
		"avg_speed_m_s":        avgSpeed,
		"duration_seconds":     dur,
		"bbox":                 []float64{minLat, maxLat, minLon, maxLon},
		"source_file":          srcFile,
		"created_at":           created,
		"updated_at":           updated,
	}
	gpxJSON(w, GPXStoreResponse{Status: "ok", Data: track}, http.StatusOK)
}

func handleGPXGetTrackPoints(w http.ResponseWriter, r *http.Request) {
	// path: /gpx/tracks/{id}/points
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/gpx/tracks/"), "/")
	if len(parts) < 2 || parts[1] != "points" {
		gpxError(w, "not found", http.StatusNotFound)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		gpxError(w, "invalid track id", http.StatusBadRequest)
		return
	}

	limit := 50000
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 200000 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 0 {
			offset = n
		}
	}

	rows, err := gpxDB.Query(
		`SELECT lat, lon, elevation, COALESCE(time,''), point_index FROM track_points
		 WHERE track_id = ? ORDER BY point_index LIMIT ? OFFSET ?`, id, limit, offset)
	if err != nil {
		gpxError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	points := make([]map[string]interface{}, 0)
	for rows.Next() {
		var lat, lon, ele float64
		var timeStr string
		var idx int
		if err := rows.Scan(&lat, &lon, &ele, &timeStr, &idx); err != nil {
			continue
		}
		points = append(points, map[string]interface{}{
			"lat":         lat,
			"lon":         lon,
			"elevation":   ele,
			"time":        timeStr,
			"point_index": idx,
		})
	}
	gpxJSON(w, GPXStoreResponse{Status: "ok", Data: points}, http.StatusOK)
}

func handleGPXDeleteTrack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		gpxError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/gpx/tracks/")
	idStr = strings.TrimSuffix(idStr, "/delete")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		gpxError(w, "invalid track id", http.StatusBadRequest)
		return
	}
	_, err = gpxDB.Exec(`DELETE FROM tracks WHERE id = ?`, id)
	if err != nil {
		gpxError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	gpxJSON(w, GPXStoreResponse{Status: "ok", Data: map[string]int64{"deleted": id}}, http.StatusOK)
}

func handleGPXSearch(w http.ResponseWriter, r *http.Request) {
	// /gpx/search?min_lat=&max_lat=&min_lon=&max_lon=
	q := r.URL.Query()
	minLat, _ := strconv.ParseFloat(q.Get("min_lat"), 64)
	maxLat, _ := strconv.ParseFloat(q.Get("max_lat"), 64)
	minLon, _ := strconv.ParseFloat(q.Get("min_lon"), 64)
	maxLon, _ := strconv.ParseFloat(q.Get("max_lon"), 64)

	query := `SELECT id, COALESCE(name,''), COALESCE(description,''), total_distance_m,
		total_elevation_gain_m, point_count, COALESCE(source_file,''), created_at
		FROM tracks WHERE 1=1`
	args := make([]interface{}, 0)
	if minLat != 0 || maxLat != 0 {
		query += ` AND bbox_max_lat >= ? AND bbox_min_lat <= ?`
		args = append(args, minLat, maxLat)
	}
	if minLon != 0 || maxLon != 0 {
		query += ` AND bbox_max_lon >= ? AND bbox_min_lon <= ?`
		args = append(args, minLon, maxLon)
	}
	if name := q.Get("name"); name != "" {
		query += ` AND name LIKE '%' || ? || '%'`
		args = append(args, name)
	}
	query += ` ORDER BY created_at DESC LIMIT 100`

	rows, err := gpxDB.Query(query, args...)
	if err != nil {
		gpxError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	tracks := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var name, desc, srcFile, created string
		var dist, elev float64
		var pts int
		if err := rows.Scan(&id, &name, &desc, &dist, &elev, &pts, &srcFile, &created); err != nil {
			continue
		}
		tracks = append(tracks, map[string]interface{}{
			"id": id, "name": name, "description": desc,
			"total_distance_m": dist, "total_elevation_gain_m": elev,
			"point_count": pts, "source_file": srcFile, "created_at": created,
		})
	}
	gpxJSON(w, GPXStoreResponse{Status: "ok", Data: tracks}, http.StatusOK)
}

func handleGPXWaypoints(w http.ResponseWriter, r *http.Request) {
	rows, err := gpxDB.Query(`SELECT id, lat, lon, COALESCE(elevation,0), COALESCE(name,''), COALESCE(description,''), COALESCE(time,''), created_at FROM waypoints ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		gpxError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	wpts := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var lat, lon, ele float64
		var name, desc, timeStr, created string
		if err := rows.Scan(&id, &lat, &lon, &ele, &name, &desc, &timeStr, &created); err != nil {
			continue
		}
		wpts = append(wpts, map[string]interface{}{
			"id": id, "lat": lat, "lon": lon, "elevation": ele,
			"name": name, "description": desc, "time": timeStr, "created_at": created,
		})
	}
	gpxJSON(w, GPXStoreResponse{Status: "ok", Data: wpts}, http.StatusOK)
}

func handleGPXStats(w http.ResponseWriter, r *http.Request) {
	var trackCount, pointCount, wptCount, routeCount int
	gpxDB.QueryRow(`SELECT COUNT(1) FROM tracks`).Scan(&trackCount)
	gpxDB.QueryRow(`SELECT COALESCE(SUM(point_count),0) FROM tracks`).Scan(&pointCount)
	gpxDB.QueryRow(`SELECT COUNT(1) FROM waypoints`).Scan(&wptCount)
	gpxDB.QueryRow(`SELECT COUNT(1) FROM gpx_routes`).Scan(&routeCount)

	var totalDist float64
	gpxDB.QueryRow(`SELECT COALESCE(SUM(total_distance_m),0) FROM tracks`).Scan(&totalDist)

	gpxJSON(w, GPXStoreResponse{Status: "ok", Data: map[string]interface{}{
		"track_count":        trackCount,
		"total_point_count":  pointCount,
		"waypoint_count":     wptCount,
		"route_count":        routeCount,
		"total_distance_km":  math.Round(totalDist/10) / 100,
	}}, http.StatusOK)
}

func bytesContainsLower(b []byte, substr string) bool {
	return strings.Contains(strings.ToLower(string(b)), substr)
}

// ─── Init ───────────────────────────────────────────────────────────────────

func initGPXStore() {
	if err := openGPXStore(); err != nil {
		log.Printf("[gpx_store] failed to open database: %v", err)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/gpx/upload", handleGPXUpload)
	mux.HandleFunc("/gpx/tracks", handleGPXListTracks)
	mux.HandleFunc("/gpx/tracks/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/points") || strings.Contains(path, "/points") {
			handleGPXGetTrackPoints(w, r)
		} else if strings.HasSuffix(path, "/export") || strings.Contains(path, "/export") {
			handleGPXGetTrack(w, r)
		} else if strings.HasSuffix(path, "/delete") {
			handleGPXDeleteTrack(w, r)
		} else {
			handleGPXGetTrack(w, r)
		}
	})
	mux.HandleFunc("/gpx/search", handleGPXSearch)
	mux.HandleFunc("/gpx/waypoints", handleGPXWaypoints)
	mux.HandleFunc("/gpx/stats", handleGPXStats)

	go func() {
		addr := ":8083"
		log.Println("GPX Store running on http://127.0.0.1" + addr)
		log.Println("  POST /gpx/upload           — upload & parse a GPX file")
		log.Println("  GET  /gpx/tracks            — list all tracks")
		log.Println("  GET  /gpx/tracks/{id}       — track details")
		log.Println("  GET  /gpx/tracks/{id}/export— export track as GPX")
		log.Println("  GET  /gpx/tracks/{id}/points— track points (paginated)")
		log.Println("  POST /gpx/tracks/{id}/delete— delete track")
		log.Println("  GET  /gpx/search?bbox=...   — search by bounding box")
		log.Println("  GET  /gpx/waypoints          — list waypoints")
		log.Println("  GET  /gpx/stats              — store statistics")
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("[gpx_store] server error: %v", err)
		}
	}()
}
