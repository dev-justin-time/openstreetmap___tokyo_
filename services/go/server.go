package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	MAX_DRIVERS          = 5000
	MAX_ORDER_QUEUE      = 10000
	ASSIGN_SWEEP_MS      = 1500
	ASSIGN_RADIUS_M      = 5000
	INITIAL_DRIVER_COUNT = 100
	DRIVER_QUALITY_MIN   = 40
	DRIVER_QUALITY_MAX   = 98
	DRIVERS_PERSIST_PATH = "drivers.json"
)

type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type RouteEngineUpdate struct {
	DriverID string                 `json:"driver_id"`
	Location LatLng                 `json:"location"`
	Status   string                 `json:"status,omitempty"`
	Online   bool                   `json:"online,omitempty"`
	Meta     map[string]interface{} `json:"meta,omitempty"`
}

type WorkOrder struct {
	OrderID    string `json:"order_id"`
	Type       string `json:"type"`
	Location   LatLng `json:"location"`
	Details    string `json:"details,omitempty"`
	Priority   int    `json:"priority,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	AssignedTo string `json:"assigned_to,omitempty"`
}

type driverRec struct {
	id         string
	loc        LatLng
	status     string
	online     bool
	lastSeen   time.Time
	eventsChan chan interface{}
	mu         sync.Mutex

	Rating         float64 `json:"rating,omitempty"`
	AcceptanceRate float64 `json:"acceptance_rate,omitempty"`
	CompletedJobs  int     `json:"completed_jobs,omitempty"`
	Efficiency     float64 `json:"efficiency,omitempty"`
}

var (
	driversMu sync.RWMutex
	drivers   = make(map[string]*driverRec)
	ordersMu  sync.Mutex
	orders    = make([]*WorkOrder, 0, 1024)
)

func metersBetween(a, b LatLng) float64 {
	const R = 6371000.0
	lat1 := a.Lat * (math.Pi / 180.0)
	lat2 := b.Lat * (math.Pi / 180.0)
	dlat := (b.Lat - a.Lat) * (math.Pi / 180.0)
	dlon := (b.Lng - a.Lng) * (math.Pi / 180.0)
	sdlat := math.Sin(dlat / 2)
	sdlon := math.Sin(dlon / 2)
	h := sdlat*sdlat + math.Cos(lat1)*math.Cos(lat2)*sdlon*sdlon
	c := 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
	return R * c
}

func driverConnectHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		http.Error(w, "bad driver path", http.StatusBadRequest)
		return
	}
	driverID := parts[2]

	driversMu.Lock()
	if len(drivers) >= MAX_DRIVERS {
		driversMu.Unlock()
		http.Error(w, "server busy", http.StatusServiceUnavailable)
		return
	}
	dr, ok := drivers[driverID]
	if !ok {
		dr = &driverRec{
			id:             driverID,
			loc:            LatLng{Lat: 0, Lng: 0},
			status:         "idle",
			online:         true,
			lastSeen:       time.Now(),
			eventsChan:     make(chan interface{}, 64),
			Rating:         float64(DRIVER_QUALITY_MIN) + rand.Float64()*float64(DRIVER_QUALITY_MAX-DRIVER_QUALITY_MIN),
			AcceptanceRate: 0.7 + rand.Float64()*0.3,
			CompletedJobs:  0,
			Efficiency:     0.5 + rand.Float64()*0.5,
		}
		drivers[driverID] = dr
	}
	driversMu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	dr.mu.Lock()
	dr.online = true
	dr.lastSeen = time.Now()
	dr.mu.Unlock()

	initial := map[string]interface{}{
		"type":         "welcome",
		"ordersQueued": len(orders),
		"driver_id":    driverID,
	}
	fmt.Fprintf(w, "data: %s\n\n", mustJSON(initial))
	flusher.Flush()

	notify := ctx.Done()
	for {
		select {
		case <-notify:
			goto cleanup
		case ev := <-dr.eventsChan:
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(ev))
			flusher.Flush()
		case <-time.After(20 * time.Second):
			fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		}
	}

cleanup:
	dr.mu.Lock()
	dr.online = false
	dr.lastSeen = time.Now()
	dr.mu.Unlock()
}

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
	var du RouteEngineUpdate
	if err := json.NewDecoder(r.Body).Decode(&du); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	driversMu.Lock()
	dr, ok := drivers[driverID]
	if !ok {
		dr = &driverRec{
			id:             driverID,
			loc:            du.Location,
			status:         "idle",
			online:         du.Online,
			lastSeen:       time.Now(),
			eventsChan:     make(chan interface{}, 64),
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
	ordersMu.Lock()
	if len(orders) >= MAX_ORDER_QUEUE {
		ordersMu.Unlock()
		http.Error(w, "order queue full", http.StatusServiceUnavailable)
		return
	}
	orders = append(orders, &o)
	ordersMu.Unlock()

	assigned := tryAssignOrders()
	resp := map[string]interface{}{"order_id": o.OrderID, "assigned_to": o.AssignedTo, "assigned": assigned}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

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
			ord.AssignedTo = bestDriver
			assignedAny = true
			go notifyDriverAssignment(bestDriver, ord)
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
	select {
	case dr.eventsChan <- ev:
	default:
	}
}

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
	go persistDriversToDisk(DRIVERS_PERSIST_PATH)
}

func startRouteEngine(addr string) {
	loadDriversFromDisk(DRIVERS_PERSIST_PATH)
	generateInitialDrivers(INITIAL_DRIVER_COUNT)

	http.HandleFunc("/driver/", func(w http.ResponseWriter, r *http.Request) {
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

	log.Printf("route-engine listening on %s (max drivers ~%d, initial pool %d)", addr, MAX_DRIVERS, INITIAL_DRIVER_COUNT)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}
