package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
	"strconv"

	_ "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Thin orchestrator: initialize DB, metrics, workers and simulator and wire HTTP handlers.
func main() {
	// initialize DB
	if err := initDB(); err != nil {
		log.Fatalf("failed to init sqlite queue: %v", err)
	}

	// register Prometheus metrics
	registerMetrics()

	// start worker pool
	startWorkerPool()

	// HTTP handlers
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/upload", instrumentHandler("upload", uploadHandler))
	http.HandleFunc("/health", instrumentHandlerFunc("health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	// driver_generator.go registers /generate-drivers and /driverhome via init()
	// expose a simple nearby gateway that queries the SQLite RTree index
	http.HandleFunc("/nearby", instrumentHandler("nearby", func(w http.ResponseWriter, r *http.Request) {
		// simple query params: ?lat=&lon=&radius_m=&limit=
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"count": len(keys),
			"drivers": keys,
		})
	}))

	// determine Rust tracker addresses
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
		instanceCount := 1
		if v := os.Getenv("INSTANCE_COUNT"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				instanceCount = n
			}
		}
		for i := 0; i < instanceCount; i++ {
			port := 3030 + i
			rustAddrs = append(rustAddrs, fmt.Sprintf("http://127.0.0.1:%d", port))
		}
	}

	// start simulator in background
	go startSimulator(1000, time.Second*1, rustAddrs)

	addr := ":8080"
	fmt.Println("Go API gateway running on http://127.0.0.1" + addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	timer := prometheus.NewTimer(promRequestDuration.WithLabelValues("upload"))
	defer timer.ObserveDuration()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		promRequestsTotal.WithLabelValues("upload", "405").Inc()
		return
	}
	// limit overall parse memory and file size to 25MB
	const maxSize = 25 << 20 // 25 MB
	err := r.ParseMultipartForm(maxSize)
	if err != nil {
		http.Error(w, "failed to parse multipart form: "+err.Error(), http.StatusBadRequest)
		promRequestsTotal.WithLabelValues("upload", "400").Inc()
		return
	}
	file, header, err := r.FormFile("gpx")
	if err != nil {
		http.Error(w, "missing gpx file", http.StatusBadRequest)
		promRequestsTotal.WithLabelValues("upload", "400").Inc()
		return
	}
	defer file.Close()

	// read up to maxSize+1 to detect oversize
	var buf bytes.Buffer
	limited := io.LimitReader(file, maxSize+1)
	n, err := io.Copy(&buf, limited)
	if err != nil {
		http.Error(w, "failed to read file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if n > maxSize {
		http.Error(w, "file too large (max 25MB)", http.StatusRequestEntityTooLarge)
		return
	}
	rawBytes := buf.Bytes()

	// basic MIME sniff
	contentType := http.DetectContentType(rawBytes)
	allowed := false
	// allow common GPX/XML content types and some XML/text fallbacks
	if strings.Contains(contentType, "xml") || strings.Contains(contentType, "text") || contentType == "application/octet-stream" {
		allowed = true
	}
	// also accept if filename suggests .gpx
	if !allowed && strings.HasSuffix(strings.ToLower(header.Filename), ".gpx") {
		allowed = true
	}
	if !allowed {
		http.Error(w, "unsupported file type: "+contentType, http.StatusUnsupportedMediaType)
		return
	}

	// lightweight sanity check: ensure it contains a <gpx element
	if !bytes.Contains(bytes.ToLower(rawBytes), []byte("<gpx")) {
		http.Error(w, "invalid GPX: missing <gpx> element", http.StatusBadRequest)
		return
	}

	// update queue depth gauge approx
	var count int
	if err := db.QueryRow("SELECT COUNT(1) FROM queue WHERE status IN ('pending','processing')").Scan(&count); err == nil {
		promQueueDepth.Set(float64(count))
	}

	// If circuit-breaker is open, respond quickly and enqueue raw as fallback
	if !breakerAllow() {
		if _, err := enqueuePrimary([]byte(`{"status":"deferred","reason":"circuit_open_on_receive"}`), rawBytes); err != nil {
			http.Error(w, "failed to enqueue fallback: "+err.Error(), http.StatusInternalServerError)
			promRequestsTotal.WithLabelValues("upload", "500").Inc()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"queued","reason":"circuit_open"}`))
		promRequestsTotal.WithLabelValues("upload", "503").Inc()
		return
	}

	// Build a job and submit to worker pool. We provide a small respChan so the HTTP request
	// can optionally wait for a short window for the primary forward response; otherwise it's queued.
	job := &forwardJob{
		rawBytes: rawBytes,
		filename: header.Filename,
		respChan: make(chan forwardResult, 1),
		attempts: 0,
	}

	select {
	case jobCh <- job:
		// wait briefly for a result so small requests get synchronous feel
		select {
		case res := <-job.respChan:
			if res.err != nil {
				// If worker reported error, return accepted and note deferred
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				w.Write([]byte(`{"status":"deferred","note":"worker_error"}`))
				return
			}
			// Return the primary service response directly
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(res.statusCode)
			w.Write(res.body)
			return
		case <-time.After(800 * time.Millisecond):
			// worker didn't respond in time; return accepted and let worker continue
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"status":"queued","note":"processing_async"}`))
			return
		}
	default:
		// job queue full -> fallback to enqueue raw immediately
		if _, err := enqueuePrimary([]byte(`{"status":"deferred","reason":"queue_full_on_receive"}`), rawBytes); err != nil {
			http.Error(w, "failed to enqueue fallback: "+err.Error(), http.StatusInternalServerError)
			promRequestsTotal.WithLabelValues("upload", "500").Inc()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"queued","reason":"queue_full"}`))
		promRequestsTotal.WithLabelValues("upload", "503").Inc()
		return
	}
}

// driverUpdate is the payload expected by the Rust tracker.
type driverUpdate struct {
	ID       string  `json:"id"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Status   string  `json:"status,omitempty"`
	TsUnixMs int64   `json:"ts_unix_ms,omitempty"`
}

/*
startSimulator launches N simulated drivers and periodically sends updates.
Instead of assuming sequential localhost ports, this version accepts a slice of Rust
tracker base addresses (e.g., "http://127.0.0.1:3030", "http://tracker-1:3030") and
uses rendezvous hashing (highest score wins) to pick the target for each driver id.
*/
func startSimulator(count int, interval time.Duration, rustAddrs []string) {
	rand.Seed(time.Now().UnixNano())
	centerLat := 19.4326
	centerLon := -99.1332

	// initialize drivers with random offsets
	type drv struct {
		id     string
		lat    float64
		lon    float64
		status string
	}
	drivers := make([]*drv, 0, count)
	for i := 0; i < count; i++ {
		offsetLat := (rand.Float64()-0.5)*0.02 // ~ +/- approx 1km
		offsetLon := (rand.Float64()-0.5)*0.02
		status := "available"
		if rand.Intn(4) == 0 {
			status = "unavailable"
		}
		drivers = append(drivers, &drv{
			id:     fmt.Sprintf("sim-%03d", i+1),
			lat:    centerLat + offsetLat,
			lon:    centerLon + offsetLon,
			status: status,
		})
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// Rendezvous hashing: compute a score for each (id, addr) and pick highest.
	scoreFor := func(id string, addr string) uint64 {
		// Use FNV-1a 64-bit mixing for simple deterministic score
		const (
			offset64 = 14695981039346656037
			prime64  = 1099511628211
		)
		var h uint64 = offset64
		// mix id
		for i := 0; i < len(id); i++ {
			h ^= uint64(id[i])
			h *= prime64
		}
		// mix separator
		h ^= 0xff
		h *= prime64
		// mix addr
		for i := 0; i < len(addr); i++ {
			h ^= uint64(addr[i])
			h *= prime64
		}
		return h
	}

	chooseAddr := func(id string) string {
		if len(rustAddrs) == 0 {
			return "http://127.0.0.1:3030"
		}
		best := uint64(0)
		bestIdx := 0
		for i, a := range rustAddrs {
			s := scoreFor(id, a)
			if i == 0 || s > best {
				best = s
				bestIdx = i
			}
		}
		return rustAddrs[bestIdx]
	}

	// batch settings
	const batchSize = 50

	for {
		// collect per-target batches to preserve rendezvous routing per id
		batches := make(map[string][]driverUpdate)

		for _, d := range drivers {
			// small random walk
			d.lat += (rand.Float64()-0.5) * 0.0015
			d.lon += (rand.Float64()-0.5) * 0.0015
			// occasionally flip status
			if rand.Float64() < 0.05 {
				if d.status == "available" {
					d.status = "unavailable"
				} else {
					d.status = "available"
				}
			}

			u := driverUpdate{
				ID:       d.id,
				Lat:      d.lat,
				Lon:      d.lon,
				Status:   d.status,
				TsUnixMs: time.Now().UnixNano() / int64(time.Millisecond),
			}

			targetBase := chooseAddr(u.ID)
			targetURL := strings.TrimRight(targetBase, "/") + "/track-batch"

			batches[targetURL] = append(batches[targetURL], u)

			// if any batch reaches batchSize, send it immediately
			if len(batches[targetURL]) >= batchSize {
				toSend := batches[targetURL]
				batches[targetURL] = nil // reset
				go func(url string, payloadBatch []driverUpdate) {
					data, _ := json.Marshal(payloadBatch)
					req, _ := http.NewRequest("POST", url, bytes.NewBuffer(data))
					req.Header.Set("Content-Type", "application/json")
					// lightweight tracing: attach incoming trace header if present in env (simulator doesn't have request context)
					// noop for now; hook for future propagation
					resp, err := client.Do(req)
					if err == nil {
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
				}(targetURL, toSend)
			}
		}

		// flush remaining batches (if any)
		for url, batch := range batches {
			if len(batch) == 0 {
				continue
			}
			// if batch is small, still send as a single POST to /track-batch
			go func(url string, payloadBatch []driverUpdate) {
				data, _ := json.Marshal(payloadBatch)
				req, _ := http.NewRequest("POST", url, bytes.NewBuffer(data))
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}(url, batch)
		}

		// update queue depth gauge periodically
		var count int
		if err := db.QueryRow("SELECT COUNT(1) FROM queue WHERE status IN ('pending','processing')").Scan(&count); err == nil {
			promQueueDepth.Set(float64(count))
		}
		time.Sleep(interval)
	}
}