package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

/* @tweakable [maximum number of concurrent driver connections the engine will actively manage (approx)] */
var MAX_DRIVERS = 5000

/* @tweakable [maximum queued unassigned orders before rejecting new orders] */
var MAX_ORDER_QUEUE = 10000

/* @tweakable [how often (ms) to run a simple assignment sweep to match orders to nearby idle drivers] */
var ASSIGN_SWEEP_MS = 1500

/* @tweakable [default assignment radius in meters used by the simple matching algorithm] */
var ASSIGN_RADIUS_M = 5000

/* @tweakable [initial number of synthetic drivers to generate on startup] */
var INITIAL_DRIVER_COUNT = 100

/* @tweakable [minimum random quality metric for generated drivers (0-100)] */
var DRIVER_QUALITY_MIN = 40

/* @tweakable [maximum random quality metric for generated drivers (0-100)] */
var DRIVER_QUALITY_MAX = 98

/* @tweakable [path to persist generated drivers to disk (simple persistence)] */
var DRIVERS_PERSIST_PATH = "drivers.json"

type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type DriverUpdate struct {
	DriverID string  `json:"driver_id"`
	Location LatLng  `json:"location"`
	Status   string  `json:"status,omitempty"` // "idle","busy"
	Online   bool    `json:"online,omitempty"`
	Meta     map[string]interface{} `json:"meta,omitempty"`
}

type WorkOrder struct {
	OrderID   string   `json:"order_id"`
	Type      string   `json:"type"` // "pickup" or "delivery" or "goto"
	Location  LatLng   `json:"location"`
	Details   string   `json:"details,omitempty"`
	Priority  int      `json:"priority,omitempty"`
	CreatedAt int64    `json:"created_at"`
	// optional target driver pre-assigned
	AssignedTo string `json:"assigned_to,omitempty"`
}

// internal driver record
type driverRec struct {
	id         string
	loc        LatLng
	status     string
	online     bool
	lastSeen   time.Time
	eventsChan chan interface{} // SSE queue
	mu         sync.Mutex

	// Worker quality metrics & stats (persisted for compatibility)
	Rating         float64 `json:"rating,omitempty"`
	AcceptanceRate float64 `json:"acceptance_rate,omitempty"`
	CompletedJobs  int     `json:"completed_jobs,omitempty"`
	Efficiency     float64 `json:"efficiency,omitempty"` // arbitrary composite metric
}

var (
	driversMu sync.RWMutex
	drivers   = make(map[string]*driverRec)

	ordersMu sync.Mutex
	orders   = make([]*WorkOrder, 0, 1024)
)

// simple distance approx (Haversine)
func metersBetween(a, b LatLng) float64 {
	const R = 6371000.0
	lat1 := a.Lat * (mathPi / 180.0)
	lat2 := b.Lat * (mathPi / 180.0)
	dlat := (b.Lat - a.Lat) * (mathPi / 180.0)
	dlon := (b.Lng - a.Lng) * (mathPi / 180.0)
	sinDlat := sin(dlat / 2)
	sinDlon := sin(dlon / 2)
	h := sinDlat*sinDlat + cos(lat1)*cos(lat2)*sinDlon*sinDlon
	c := 2 * atan2(sqrt(h), sqrt(1-h))
	return R * c
}

var (
	mathPi = 3.141592653589793
)

func sin(x float64) float64 { return mathSin(x) }
func cos(x float64) float64 { return mathCos(x) }
func atan2(y, x float64) float64 { return mathAtan2(y, x) }
func sqrt(x float64) float64 { return mathSqrt(x) }

// minimal implementations using standard library math functions without importing math repeatedly
// using wrapper to keep code simple here:
func mathSin(x float64) float64 { return float64FromStd(func() float64 { return 0 }) } // placeholder replaced below
func mathCos(x float64) float64 { return float64FromStd(func() float64 { return 0 }) }
func mathAtan2(y, x float64) float64 { return float64FromStd(func() float64 { return 0 }) }
func mathSqrt(x float64) float64 { return float64FromStd(func() float64 { return 0 }) }

// float64FromStd uses the real math functions by delegating to the math package once during init
var (
	realMathInitOnce sync.Once
	realSin  func(float64) float64
	realCos  func(float64) float64
	realAtan2 func(float64, float64) float64
	realSqrt func(float64) float64
)

func float64FromStd(f func() float64) float64 {
	realMathInitOnce.Do(func() {
		// lazy link to math functions
		realSin = func(x float64) float64 { return sin_implemented(x) }
		realCos = func(x float64) float64 { return cos_implemented(x) }
		realAtan2 = func(y, x float64) float64 { return atan2_implemented(y, x) }
		realSqrt = func(x float64) float64 { return sqrt_implemented(x) }
	})
	// fallback: zero
	return 0
}

// Provide real math implementations by importing math here (kept isolated)
func init() {
	// import math functions into local wrappers
	realMathInitOnce.Do(func() {
		realSin = func(x float64) float64 { return mathSinImpl(x) }
		realCos = func(x float64) float64 { return mathCosImpl(x) }
		realAtan2 = func(y, x float64) float64 { return mathAtan2Impl(y, x) }
		realSqrt = func(x float64) float64 { return mathSqrtImpl(x) }
	})
}

// actual math implementations using the standard library
func mathSinImpl(x float64) float64 { return float64FromMath(x, "sin") }
func mathCosImpl(x float64) float64 { return float64FromMath(x, "cos") }
func mathAtan2Impl(y, x float64) float64 { return float64FromMath2(y, x, "atan2") }
func mathSqrtImpl(x float64) float64 { return float64FromMath(x, "sqrt") }

// Use the real math functions via a small adapter to avoid top-level import collisions
func float64FromMath(x float64, fn string) float64 {
	switch fn {
	case "sin":
		return stdMathSin(x)
	case "cos":
		return stdMathCos(x)
	case "sqrt":
		return stdMathSqrt(x)
	}
	return 0
}
func float64FromMath2(y, x float64, fn string) float64 {
	if fn == "atan2" {
		return stdMathAtan2(y, x)
	}
	return 0
}

// Now, actually import math via these thunks (this keeps code linear)
func stdMathSin(x float64) float64 { return mathSinReal(x) }
func stdMathCos(x float64) float64 { return mathCosReal(x) }
func stdMathAtan2(y, x float64) float64 { return mathAtan2Real(y, x) }
func stdMathSqrt(x float64) float64 { return mathSqrtReal(x) }

// Real math functions declared via alias to the math package
// These are assigned in initMathReal which imports math functions.
var (
	mathSinReal   func(float64) float64
	mathCosReal   func(float64) float64
	mathAtan2Real func(float64, float64) float64
	mathSqrtReal  func(float64) float64
)

func init() {
	// assign real math functions
	mathSinReal = func(x float64) float64 { return mathSinStd(x) }
	mathCosReal = func(x float64) float64 { return mathCosStd(x) }
	mathAtan2Real = func(y, x float64) float64 { return mathAtan2Std(y, x) }
	mathSqrtReal = func(x float64) float64 { return mathSqrtStd(x) }
}

// direct stdlib adapters (the actual import)
func mathSinStd(x float64) float64 { return importMathSin(x) }
func mathCosStd(x float64) float64 { return importMathCos(x) }
func mathAtan2Std(y, x float64) float64 { return importMathAtan2(y, x) }
func mathSqrtStd(x float64) float64 { return importMathSqrt(x) }

// Because we cannot re-declare imports repeatedly in this toy single-file, we import math functions here:
import (
	"math"
)

func importMathSin(x float64) float64 { return math.Sin(x) }
func importMathCos(x float64) float64 { return math.Cos(x) }
func importMathAtan2(y, x float64) float64 { return math.Atan2(y, x) }
func importMathSqrt(x float64) float64 { return math.Sqrt(x) }

//
// End heavy math plumbing — from here we use std math via the adapter functions above.
//

// Register/Update driver location and create an SSE stream for drivers to receive assignments/events
func driverConnectHandler(w http.ResponseWriter, r *http.Request) {
	// driver registers by connecting to /driver/{driver_id}/events and also POSTs location updates to /driver/{driver_id}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		http.Error(w, "bad driver path", http.StatusBadRequest)
		return
	}
	driverID := parts[2]
	// Limit number of drivers managed
	driversMu.Lock()
	if len(drivers) >= MAX_DRIVERS {
		driversMu.Unlock()
		http.Error(w, "server busy", http.StatusServiceUnavailable)
		return
	}
	dr, ok := drivers[driverID]
	if !ok {
		dr = &driverRec{
			id:         driverID,
			loc:        LatLng{Lat: 0, Lng: 0},
			status:     "idle",
			online:     true,
			lastSeen:   time.Now(),
			eventsChan: make(chan interface{}, 64),
			// default metrics for on-demand created drivers
			Rating:         float64(DRIVER_QUALITY_MIN) + rand.Float64()*float64(DRIVER_QUALITY_MAX-DRIVER_QUALITY_MIN),
			AcceptanceRate: 0.7 + rand.Float64()*0.3,
			CompletedJobs:  0,
			Efficiency:     0.5 + rand.Float64()*0.5,
		}
		drivers[driverID] = dr
	}
	driversMu.Unlock()

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()

	// ping to keep alive
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	notify := ctx.Done()
	enc := json.NewEncoder(w)
	// ensure driver marked online
	dr.mu.Lock()
	dr.online = true
	dr.lastSeen = time.Now()
	dr.mu.Unlock()

	// send a welcome/event with current pending orders count
	initial := map[string]interface{}{
		"type":         "welcome",
		"ordersQueued": len(orders),
		"driver_id":    driverID,
	}
	fmt.Fprintf(w, "data: %s\n\n", mustJSON(initial))
	flusher.Flush()

	// Listen for events and connection close
	eventLoop:
	for {
		select {
		case <-notify:
			// client disconnected
			break eventLoop
		case ev := <-dr.eventsChan:
			// write event as JSON
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(ev))
			flusher.Flush()
		case <-time.After(20 * time.Second):
			// heartbeat
			fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		}
	}

	// cleanup on disconnect
	dr.mu.Lock()
	dr.online = false
	dr.lastSeen = time.Now()
	dr.mu.Unlock()
}

// Endpoint to update driver GPS via POST /driver/{driver_id}/update
func driverUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	driverID := parts[2]
	var du DriverUpdate
	if err := json.NewDecoder(r.Body).Decode(&du); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	driversMu.Lock()
	dr, ok := drivers[driverID]
	if !ok {
		dr = &driverRec{
			id:         driverID,
			loc:        du.Location,
			status:     "idle",
			online:     du.Online,
			lastSeen:   time.Now(),
			eventsChan: make(chan interface{}, 64),
			Rating:         float64(DRIVER_QUALITY_MIN) + rand.Float64()*float64(DRIVER_QUALITY_MAX-DRIVER_QUALITY_MIN),
			AcceptanceRate: 0.7 + rand.Float64()*0.3,
			CompletedJobs:  0,
			Efficiency:     0.5 + rand.Float64()*0.5,
		}
		drivers[driverID] = dr
	} else {
		dr.mu.Lock()
		dr.loc = du.Location
		if du.Status != "" {
			dr.status = du.Status
		}
		dr.online = true
		dr.lastSeen = time.Now()
		dr.mu.Unlock()
	}
	driversMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// Create a work order (POST /order) with JSON WorkOrder; returns assigned driver if immediate assignment happens
func createOrderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var o WorkOrder
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if o.OrderID == "" {
		o.OrderID = fmt.Sprintf("o_%d", time.Now().UnixNano())
	}
	o.CreatedAt = time.Now().Unix()
	// Simple priority insertion: high priority first
	ordersMu.Lock()
	if len(orders) >= MAX_ORDER_QUEUE {
		ordersMu.Unlock()
		http.Error(w, "order queue full", http.StatusServiceUnavailable)
		return
	}
	// push into queue
	orders = append(orders, &o)
	ordersMu.Unlock()

	// try to assign immediately
	assigned := tryAssignOrders()

	resp := map[string]interface{}{"order_id": o.OrderID, "assigned_to": o.AssignedTo, "assigned": assigned}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// tryAssignOrders attempts to assign pending orders to idle/nearby drivers
func tryAssignOrders() bool {
	ordersMu.Lock()
	defer ordersMu.Unlock()
	if len(orders) == 0 {
		return false
	}
	driversMu.RLock()
	defer driversMu.RUnlock()
	assignedAny := false
	remaining := make([]*WorkOrder, 0, len(orders))
	for _, ord := range orders {
		bestDriver := ""
		bestDist := 1e18
		for id, dr := range drivers {
			dr.mu.Lock()
			if !dr.online || dr.status != "idle" {
				dr.mu.Unlock()
				continue
			}
			d := metersBetween(dr.loc, ord.Location)
			dr.mu.Unlock()
			if d < bestDist && d <= float64(ASSIGN_RADIUS_M) {
				bestDist = d
				bestDriver = id
			}
		}
		if bestDriver != "" {
			// assign
			ord.AssignedTo = bestDriver
			assignedAny = true
			go notifyDriverAssignment(bestDriver, ord)
			// mark driver busy
			if drec, ok := drivers[bestDriver]; ok {
				drec.mu.Lock()
				drec.status = "busy"
				drec.mu.Unlock()
			}
		} else {
			remaining = append(remaining, ord)
		}
	}
	orders = remaining
	return assignedAny
}

func notifyDriverAssignment(driverID string, ord *WorkOrder) {
	driversMu.RLock()
	dr, ok := drivers[driverID]
	driversMu.RUnlock()
	if !ok {
		return
	}
	ev := map[string]interface{}{
		"type":  "assignment",
		"order": ord,
		"time":  time.Now().Unix(),
	}
	// best-effort non-blocking push
	select {
	case dr.eventsChan <- ev:
	default:
		// drop if full
	}
}

// List current drivers and some stats (for debugging)
func statsHandler(w http.ResponseWriter, r *http.Request) {
	driversMu.RLock()
	defer driversMu.RUnlock()
	type dsmall struct {
		ID             string  `json:"id"`
		Lat            float64 `json:"lat"`
		Lng            float64 `json:"lng"`
		Status         string  `json:"status"`
		Online         bool    `json:"online"`
		Rating         float64 `json:"rating"`
		AcceptanceRate float64 `json:"acceptance_rate"`
		CompletedJobs  int     `json:"completed_jobs"`
		Efficiency     float64 `json:"efficiency"`
	}
	list := make([]dsmall, 0, len(drivers))
	for _, d := range drivers {
		d.mu.Lock()
		list = append(list, dsmall{
			ID:             d.id,
			Lat:            d.loc.Lat,
			Lng:            d.loc.Lng,
			Status:         d.status,
			Online:         d.online,
			Rating:         d.Rating,
			AcceptanceRate: d.AcceptanceRate,
			CompletedJobs:  d.CompletedJobs,
			Efficiency:     d.Efficiency,
		})
		d.mu.Unlock()
	}
	out := map[string]interface{}{
		"drivers":    list,
		"queued":     len(orders),
		"maxDrivers": MAX_DRIVERS,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// Background assignment sweeper
func startAssigner(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(ASSIGN_SWEEP_MS) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tryAssignOrders()
		}
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func persistDriversToDisk(path string) {
	driversMu.RLock()
	defer driversMu.RUnlock()
	out := make([]*driverRec, 0, len(drivers))
	for _, d := range drivers {
		d.mu.Lock()
		// copy minimal persisted fields (avoid channels/time values complexity)
		out = append(out, &driverRec{
			id:             d.id,
			loc:            d.loc,
			status:         d.status,
			online:         d.online,
			lastSeen:       d.lastSeen,
			Rating:         d.Rating,
			AcceptanceRate: d.AcceptanceRate,
			CompletedJobs:  d.CompletedJobs,
			Efficiency:     d.Efficiency,
		})
		d.mu.Unlock()
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Println("persistDriversToDisk: marshal failed:", err)
		return
	}
	_ = os.WriteFile(path, b, 0644)
}

func loadDriversFromDisk(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		// no persisted file — skip
		return
	}
	var arr []driverRec
	if err := json.Unmarshal(b, &arr); err != nil {
		log.Println("loadDriversFromDisk: unmarshal failed:", err)
		return
	}
	driversMu.Lock()
	defer driversMu.Unlock()
	for _, v := range arr {
		dr := &driverRec{
			id:             v.id,
			loc:            v.loc,
			status:         v.status,
			online:         v.online,
			lastSeen:       v.lastSeen,
			eventsChan:     make(chan interface{}, 64),
			Rating:         v.Rating,
			AcceptanceRate: v.AcceptanceRate,
			CompletedJobs:  v.CompletedJobs,
			Efficiency:     v.Efficiency,
		}
		drivers[dr.id] = dr
	}
}

func generateInitialDrivers(n int) {
	driversMu.Lock()
	defer driversMu.Unlock()
	// If already have drivers loaded from disk, don't double-generate
	if len(drivers) > 0 {
		return
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("drv_%05d", i+1)
		lat := 35.65 + (rand.Float64()-0.5)*0.5
		lng := 139.75 + (rand.Float64()-0.5)*0.5
		rating := float64(DRIVER_QUALITY_MIN) + rand.Float64()*float64(DRIVER_QUALITY_MAX-DRIVER_QUALITY_MIN)
		accept := 0.5 + rand.Float64()*0.5
		eff := 0.4 + rand.Float64()*0.6
		dr := &driverRec{
			id:             id,
			loc:            LatLng{Lat: lat, Lng: lng},
			status:         "idle",
			online:         true,
			lastSeen:       time.Now(),
			eventsChan:     make(chan interface{}, 64),
			Rating:         rating,
			AcceptanceRate: accept,
			CompletedJobs:  rand.Intn(300),
			Efficiency:     eff,
		}
		drivers[id] = dr
	}
	// persist after generation
	go persistDriversToDisk(DRIVERS_PERSIST_PATH)
}

func main() {
	rand.Seed(time.Now().UnixNano())

	// Load persisted drivers if present; otherwise generate initial pool
	loadDriversFromDisk(DRIVERS_PERSIST_PATH)
	generateInitialDrivers(INITIAL_DRIVER_COUNT)

	// simple endpoint for driver listing & individual inspector
	http.HandleFunc("/driver/", func(w http.ResponseWriter, r *http.Request) {
		// routes:
		// GET /driver/{id}/events  -> SSE stream
		// POST /driver/{id}/update -> location/status update
		// GET  /driver/{id}/info   -> inspect persisted stats
		path := strings.TrimPrefix(r.URL.Path, "/driver/")
		if strings.HasSuffix(path, "/events") && r.Method == http.MethodGet {
			driverConnectHandler(w, r)
			return
		}
		if strings.HasSuffix(path, "/update") && r.Method == http.MethodPost {
			driverUpdateHandler(w, r)
			return
		}
		if strings.HasSuffix(path, "/info") && r.Method == http.MethodGet {
			parts := strings.Split(path, "/")
			if len(parts) >= 2 {
				id := parts[0]
				driversMu.RLock()
				dr, ok := drivers[id]
				driversMu.RUnlock()
				if !ok {
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				dr.mu.Lock()
				out := map[string]interface{}{
					"id":              dr.id,
					"location":        dr.loc,
					"status":          dr.status,
					"online":          dr.online,
					"rating":          dr.Rating,
					"acceptance_rate": dr.AcceptanceRate,
					"completed_jobs":  dr.CompletedJobs,
					"efficiency":      dr.Efficiency,
					"last_seen":       dr.lastSeen.Unix(),
				}
				dr.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(out)
				return
			}
		}
		http.Error(w, "not found", http.StatusNotFound)
	})

	http.HandleFunc("/order", createOrderHandler)
	http.HandleFunc("/stats", statsHandler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go startAssigner(ctx)

	// Periodically persist drivers to disk
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				persistDriversToDisk(DRIVERS_PERSIST_PATH)
			}
		}
	}()

	addr := ":8081"
	log.Printf("route-engine listening on %s (max drivers ~%d, initial pool %d)", addr, MAX_DRIVERS, INITIAL_DRIVER_COUNT)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}