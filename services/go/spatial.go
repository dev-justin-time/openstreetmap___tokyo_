package main

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

// UpsertDriverIndex inserts or updates a driver location into the driver_index table
// and ensures the RTree is kept in sync via the DB triggers created at init.
func UpsertDriverIndex(driverKey string, lat, lon float64, tsUnix int64) error {
	if db == nil {
		return fmt.Errorf("db not initialized")
	}
	// Try update first
	res, err := db.Exec("UPDATE driver_index SET lat = ?, lon = ?, ts = ? WHERE driver_key = ?", lat, lon, tsUnix, driverKey)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected > 0 {
		return nil
	}
	// insert if not updated
	_, err = db.Exec("INSERT OR REPLACE INTO driver_index (driver_key, lat, lon, ts) VALUES (?, ?, ?, ?)", driverKey, lat, lon, tsUnix)
	return err
}

// QueryNearbyDrivers returns driver_key strings within radiusMeters of the given lat/lon.
// Uses an approximate conversion: degrees ~= meters / 111320 for lat; lon scaled by cos(lat).
func QueryNearbyDrivers(lat, lon float64, radiusMeters float64, limit int) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("db not initialized")
	}
	// convert radius meters to degree box (conservative)
	const metersPerDeg = 111320.0
	latDeg := radiusMeters / metersPerDeg
	lonDeg := radiusMeters / (metersPerDeg * math.Max(0.000001, math.Cos(lat*math.Pi/180.0)))

	minLat := lat - latDeg
	maxLat := lat + latDeg
	minLon := lon - lonDeg
	maxLon := lon + lonDeg

	query := `SELECT di.driver_key FROM driver_rtree dr JOIN driver_index di ON dr.rowid = di.rowid
	          WHERE dr.min_lat >= ? AND dr.max_lat <= ? AND dr.min_lon >= ? AND dr.max_lon <= ?
	          LIMIT ?`
	rows, err := db.Query(query, minLat, maxLat, minLon, maxLon, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			continue
		}
		out = append(out, key)
	}
	return out, nil
}

// Helper to upsert using driverUpdate-like struct to be used by any caller
func UpsertDriverPositionFromPayload(driverKey string, lat, lon float64) {
	_ = UpsertDriverIndex(driverKey, lat, lon, time.Now().Unix())
}