package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Initialize SQLite queue
	if err := initDB(); err != nil {
		log.Fatalf("failed to init sqlite queue: %v", err)
	}

	// Register Prometheus metrics
	registerMetrics()

	// Start worker pool for forwarding GPX to Rust
	startWorkerPool()

	// Gateway HTTP handlers (port 8080)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/upload", instrumentHandler("upload", uploadHandler))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/nearby", instrumentHandler("nearby", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		lat := 0.0
		lon := 0.0
		rad := 1000.0
		limit := 50
		if v := q.Get("lat"); v != "" {
			fmt.Sscanf(v, "%f", &lat)
		}
		if v := q.Get("lon"); v != "" {
			fmt.Sscanf(v, "%f", &lon)
		}
		if v := q.Get("radius_m"); v != "" {
			fmt.Sscanf(v, "%f", &rad)
		}
		if v := q.Get("limit"); v != "" {
			fmt.Sscanf(v, "%d", &limit)
		}
		keys, err := QueryNearbyDrivers(lat, lon, rad, limit)
		if err != nil {
			http.Error(w, "nearby query failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"count":%d,"drivers":%s}`, len(keys), func() string {
			b, _ := json.Marshal(keys)
			return string(b)
		}())
	}))

	// Determine Rust tracker addresses from env or defaults
	var rustAddrs []string
	if addrs := os.Getenv("RUST_INSTANCES"); addrs != "" {
		for _, a := range strings.Split(addrs, ",") {
			trimmed := strings.TrimSpace(a)
			if trimmed != "" {
				if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
					trimmed = "http://" + trimmed
				}
				rustAddrs = append(rustAddrs, trimmed)
			}
		}
	}
	if len(rustAddrs) == 0 {
		rustAddrs = append(rustAddrs, "http://127.0.0.1:3030")
	}

	// Start simulator in background (NYC DriverManager or basic)
	if os.Getenv("NYC_DEMO") == "1" {
		go startNYCSimulator(rustAddrs)
	} else {
		go startSimulator(1000, time.Second*1, rustAddrs)
	}

	// Start route-engine on port 8081 in background
	go startRouteEngine(":8081")

	// Start logistics API (orders, dispatch) on port 8082
	initLogistics()

	// Start GPX Store (parse, persist, export GPX files) on port 8083
	initGPXStore()

	// NYC DriverManager HTTP endpoints on gateway
	if os.Getenv("NYC_DEMO") == "1" {
		mux.HandleFunc("/nyc/drivers", handleNYCDrivers)
		mux.HandleFunc("/nyc/stats", handleNYCStats)
	}

	// Start gateway on port 8080
	addr := ":8080"
	fmt.Println("Go API gateway running on http://127.0.0.1" + addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
