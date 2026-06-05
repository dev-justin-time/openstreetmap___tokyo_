package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Simple driver record saved to driverhome.db (JSON)
type DriverRecord struct {
	ID     string  `json:"id"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Status string  `json:"status"`
	Ts     int64   `json:"ts_unix_ms"`
}

// Adds endpoints to the existing Go gateway to generate driver locations and read the stored file.
// Use /generate-drivers?count=1000&radius_miles=50 to generate; GET /driverhome returns the saved file.

func init() {
	// attach handlers to default mux used in main.go
	http.HandleFunc("/generate-drivers", generateDriversHandler)
	http.HandleFunc("/driverhome", driverHomeHandler)
}

func generateDriversHandler(w http.ResponseWriter, r *http.Request) {
	// allow only GET for generation convenience
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// parse params
	q := r.URL.Query()
	count := 1000
	radiusMiles := 50.0
	if v := q.Get("count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			count = n
		}
	}
	if v := q.Get("radius_miles"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			radiusMiles = f
		}
	}

	// center point (same default as other services)
	centerLat := 19.4326
	centerLon := -99.1332

	drivers := generateEvenlyDispersedDrivers(count, centerLat, centerLon, radiusMiles)
	// write to driverhome.db as JSON
	outFile := "driverhome.db"
	f, err := os.OpenFile(outFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		http.Error(w, "failed to open driverhome.db: "+err.Error(), http.StatusInternalServerError)
		return
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(drivers); err != nil {
		f.Close()
		http.Error(w, "failed to write driverhome.db: "+err.Error(), http.StatusInternalServerError)
		return
	}
	f.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":        len(drivers),
		"radius_miles": radiusMiles,
		"file":         outFile,
	})
}

func driverHomeHandler(w http.ResponseWriter, r *http.Request) {
	// return raw file content if exists
	fname := "driverhome.db"
	if _, err := os.Stat(fname); os.IsNotExist(err) {
		http.Error(w, "driverhome.db not found; generate first via /generate-drivers", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, fname)
}

func generateEvenlyDispersedDrivers(count int, centerLat, centerLon, radiusMiles float64) []DriverRecord {
	drivers := make([]DriverRecord, 0, count)

	// Convert miles to meters
	radiusMeters := radiusMiles * 1609.344

	// Use Vogel's method (golden angle) to evenly disperse points in a disk
	goldenAngle := math.Pi * (3 - math.Sqrt(5))

	for i := 0; i < count; i++ {
		// radius fraction (sqrt to make area uniform)
		rFrac := math.Sqrt(float64(i+1) / float64(count))
		rDist := rFrac * radiusMeters

		theta := float64(i) * goldenAngle

		// bearing in degrees
		bearing := theta * 180.0 / math.Pi

		lat, lon := destinationPoint(centerLat, centerLon, rDist, bearing)
		status := "available"
		if rand.Intn(2) == 0 {
			status = "unavailable"
		}
		d := DriverRecord{
			ID:     fmt.Sprintf("gen-%06d", i+1),
			Lat:    lat,
			Lon:    lon,
			Status: status,
			Ts:     time.Now().UnixNano() / int64(time.Millisecond),
		}
		drivers = append(drivers, d)
	}

	return drivers
}

// destinationPoint computes lat/lon given start lat/lon, distance in meters and bearing in degrees.
// Uses spherical Earth model (approx).
func destinationPoint(lat, lon, distanceMeters, bearingDeg float64) (float64, float64) {
	// Convert to radians
	latRad := deg2rad(lat)
	lonRad := deg2rad(lon)
	bearing := deg2rad(bearingDeg)
	R := 6371000.0 // earth radius in meters

	angDist := distanceMeters / R

	newLat := math.Asin(math.Sin(latRad)*math.Cos(angDist) + math.Cos(latRad)*math.Sin(angDist)*math.Cos(bearing))
	newLon := lonRad + math.Atan2(math.Sin(bearing)*math.Sin(angDist)*math.Cos(latRad), math.Cos(angDist)-math.Sin(latRad)*math.Sin(newLat))

	return rad2deg(newLat), rad2deg(newLon)
}

func deg2rad(d float64) float64 { return d * math.Pi / 180.0 }
func rad2deg(r float64) float64 { return r * 180.0 / math.Pi }