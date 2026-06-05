package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"golang.org/x/time/rate"
)

// ─── Order types ────────────────────────────────────────────────────────────

type OrderStatus string

const (
	OrderPending   OrderStatus = "pending"
	OrderAssigned  OrderStatus = "assigned"
	OrderPickedUp  OrderStatus = "picked_up"
	OrderDelivered OrderStatus = "delivered"
	OrderCancelled OrderStatus = "cancelled"
)

type Order struct {
	ID            string      `json:"id"`
	PickupLat     float64     `json:"pickup_lat"`
	PickupLon     float64     `json:"pickup_lon"`
	DropoffLat    float64     `json:"dropoff_lat"`
	DropoffLon    float64     `json:"dropoff_lon"`
	Status        OrderStatus `json:"status"`
	DriverID      string      `json:"driver_id,omitempty"`
	CreatedAt     int64       `json:"created_at_unix_ms"`
	AssignedAt    int64       `json:"assigned_at_unix_ms,omitempty"`
	DeliveredAt   int64       `json:"delivered_at_unix_ms,omitempty"`
	PickupAddr    string      `json:"pickup_addr,omitempty"`
	DropoffAddr   string      `json:"dropoff_addr,omitempty"`
	EstimatedDist float64     `json:"estimated_dist_m,omitempty"`
	EstimatedDur  float64     `json:"estimated_dur_sec,omitempty"`
}

// ─── In-memory order store ──────────────────────────────────────────────────

type OrderStore struct {
	mu     sync.RWMutex
	orders map[string]*Order
}

func NewOrderStore() *OrderStore {
	return &OrderStore{orders: make(map[string]*Order)}
}

func (s *OrderStore) Create(o *Order) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o.CreatedAt = time.Now().UnixMilli()
	s.orders[o.ID] = o
}

func (s *OrderStore) Get(id string) *Order {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.orders[id]
}

func (s *OrderStore) ListByStatus(status OrderStatus) []*Order {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Order, 0)
	for _, o := range s.orders {
		if o.Status == status {
			out = append(out, o)
		}
	}
	return out
}

func (s *OrderStore) ListAll() []*Order {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Order, 0, len(s.orders))
	for _, o := range s.orders {
		out = append(out, o)
	}
	return out
}

func (s *OrderStore) UpdateStatus(id string, status OrderStatus, driverID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[id]
	if !ok {
		return false
	}
	now := time.Now().UnixMilli()
	o.Status = status
	if driverID != "" {
		o.DriverID = driverID
		o.AssignedAt = now
	}
	if status == OrderDelivered {
		o.DeliveredAt = now
	}
	return true
}

// ─── WebSocket hub (driver push notifications) ───────────────────────────────

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type WSClient struct {
	ID       string
	DriverID string
	conn     *websocket.Conn
	done     chan struct{}
	mu       sync.Mutex // protects write on conn
}

type WSHub struct {
	mu      sync.RWMutex
	clients map[string]*WSClient // keyed by driver_id
}

var wsHub = &WSHub{clients: make(map[string]*WSClient)}

// Register adds a WebSocket client keyed by driver ID.
func (h *WSHub) Register(client *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client.DriverID] = client
	log.Printf("[ws] driver %s connected", client.DriverID)
}

// Unregister removes a WebSocket client.
func (h *WSHub) Unregister(driverID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.clients[driverID]; ok {
		close(c.done)
		delete(h.clients, driverID)
		log.Printf("[ws] driver %s disconnected", driverID)
	}
}

// BroadcastToDriver sends a JSON message to a specific driver's WebSocket.
func (h *WSHub) BroadcastToDriver(driverID string, msg []byte) {
	h.mu.RLock()
	client, ok := h.clients[driverID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case <-client.done:
		h.Unregister(driverID)
	default:
		client.mu.Lock()
		defer client.mu.Unlock()
		client.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := client.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("[ws] write error to %s: %v", driverID, err)
			h.Unregister(driverID)
		}
	}
}

// BroadcastToAll sends a JSON message to all connected drivers.
func (h *WSHub) BroadcastToAll(msg []byte) {
	h.mu.RLock()
	clients := make([]*WSClient, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		h.BroadcastToDriver(c.DriverID, msg)
	}
}



// ─── SSE fallback for web clients ─────────────────────────────────────────────

type SSEBroker struct {
	mu      sync.RWMutex
	clients map[string]chan string
}

var sseBroker = &SSEBroker{clients: make(map[string]chan string)}

func (b *SSEBroker) AddClient(id string) chan string {
	ch := make(chan string, 16)
	b.mu.Lock()
	b.clients[id] = ch
	b.mu.Unlock()
	return ch
}

func (b *SSEBroker) RemoveClient(id string) {
	b.mu.Lock()
	delete(b.clients, id)
	b.mu.Unlock()
}

func (b *SSEBroker) Broadcast(event string, data string) {
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	b.mu.RLock()
	chans := make([]chan string, 0, len(b.clients))
	for _, ch := range b.clients {
		chans = append(chans, ch)
	}
	b.mu.RUnlock()
	for _, ch := range chans {
		select {
		case ch <- msg:
		default:
		}
	}
}

func handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id := fmt.Sprintf("sse-%d", time.Now().UnixNano())
	ch := sseBroker.AddClient(id)
	defer sseBroker.RemoveClient(id)

	ctx := r.Context()
	for {
		select {
		case msg := <-ch:
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

// ─── REST handlers ──────────────────────────────────────────────────────────

var orderStore = NewOrderStore()

// POST /api/orders — create a new order
func handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PickupLat   float64 `json:"pickup_lat"`
		PickupLon   float64 `json:"pickup_lon"`
		DropoffLat  float64 `json:"dropoff_lat"`
		DropoffLon  float64 `json:"dropoff_lon"`
		PickupAddr  string  `json:"pickup_addr,omitempty"`
		DropoffAddr string  `json:"dropoff_addr,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.PickupLat == 0 && req.PickupLon == 0 {
		http.Error(w, `{"error":"pickup_coords_required"}`, http.StatusBadRequest)
		return
	}
	if req.DropoffLat == 0 && req.DropoffLon == 0 {
		http.Error(w, `{"error":"dropoff_coords_required"}`, http.StatusBadRequest)
		return
	}
	order := &Order{
		ID:         fmt.Sprintf("ord-%d", time.Now().UnixNano()),
		PickupLat:  req.PickupLat,
		PickupLon:  req.PickupLon,
		DropoffLat: req.DropoffLat,
		DropoffLon: req.DropoffLon,
		PickupAddr: req.PickupAddr,
		DropoffAddr: req.DropoffAddr,
		Status:     OrderPending,
	}
	// Estimate distance via OSRM (best-effort)
	estDist, estDur := estimateRoute(req.PickupLat, req.PickupLon, req.DropoffLat, req.DropoffLon)
	order.EstimatedDist = estDist
	order.EstimatedDur = estDur

	orderStore.Create(order)
	promOrdersCreated.Inc()

	resp, _ := json.Marshal(order)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write(resp)
}

// GET /api/orders?status=pending — list orders, optionally filtered
func handleListOrders(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	var orders []*Order
	if statusFilter != "" {
		orders = orderStore.ListByStatus(OrderStatus(statusFilter))
	} else {
		orders = orderStore.ListAll()
	}
	resp, _ := json.Marshal(map[string]interface{}{
		"count":  len(orders),
		"orders": orders,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

// POST /api/dispatch — assign nearest available driver to a pending order
func handleDispatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	order := orderStore.Get(req.OrderID)
	if order == nil {
		http.Error(w, `{"error":"order_not_found"}`, http.StatusNotFound)
		return
	}
	if order.Status != OrderPending {
		http.Error(w, `{"error":"order_not_pending"}`, http.StatusConflict)
		return
	}

	var assignedID string

	// When NYC Demo is active, use the DriverManager for dispatch
	if os.Getenv("NYC_DEMO") == "1" && nycDriverManager != nil {
		drivers := nycDriverManager.GetAllDrivers()
		var bestDriver *Driver
		bestDist := math.MaxFloat64
		for _, d := range drivers {
			if d.Status != "available" {
				continue
			}
			dist := haversine(order.PickupLat, order.PickupLon, d.Lat, d.Lng)
			if dist < bestDist {
				bestDist = dist
				bestDriver = &d
			}
		}
		if bestDriver == nil {
			http.Error(w, `{"error":"no_available_drivers"}`, http.StatusNotFound)
			return
		}
		assignedID = bestDriver.ID

		// Assign pickup as the driver's destination
		nycDriverManager.AssignDestination(assignedID, &Destination{
			Lat: order.PickupLat,
			Lng: order.PickupLon,
		})
	} else {
		// Query nearby drivers from SQLite RTree
		drivers, err := QueryNearbyDrivers(order.PickupLat, order.PickupLon, 5000, 20)
		if err != nil || len(drivers) == 0 {
			// Fallback: query Rust's in-memory nearby endpoint
			drivers = queryRustNearby(order.PickupLat, order.PickupLon, 5000)
			if len(drivers) == 0 {
				http.Error(w, `{"error":"no_nearby_drivers"}`, http.StatusNotFound)
				return
			}
		}
		assignedID = drivers[0]
	}

	orderStore.UpdateStatus(order.ID, OrderAssigned, assignedID)
	promOrdersDispatched.Inc()

	// Broadcast dispatch event via WebSocket
	event, _ := json.Marshal(map[string]interface{}{
		"type":    "dispatch",
		"order":   order,
		"driver":  assignedID,
	})
	wsHub.BroadcastToDriver(assignedID, event)
	sseBroker.Broadcast("dispatch", string(event))

	resp, _ := json.Marshal(map[string]interface{}{
		"status":    "assigned",
		"order_id":  order.ID,
		"driver_id": assignedID,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

// PUT /api/orders/:id/status — update order status (picked_up, delivered, cancelled)
func handleUpdateOrderStatus(w http.ResponseWriter, r *http.Request) {
	// Parse order ID from path: /api/orders/{id}/status
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		http.Error(w, `{"error":"invalid_path"}`, http.StatusBadRequest)
		return
	}
	orderID := parts[2]
	var req struct {
		Status   string `json:"status"`
		DriverID string `json:"driver_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if !orderStore.UpdateStatus(orderID, OrderStatus(req.Status), req.DriverID) {
		http.Error(w, `{"error":"order_not_found"}`, http.StatusNotFound)
		return
	}

	// Update driver destination on status change (NYC_DEMO mode)
	if os.Getenv("NYC_DEMO") == "1" && nycDriverManager != nil && req.DriverID != "" {
		order := orderStore.Get(orderID)
		if order != nil {
			switch OrderStatus(req.Status) {
			case OrderPickedUp:
				// Set destination to dropoff
				nycDriverManager.AssignDestination(req.DriverID, &Destination{
					Lat: order.DropoffLat,
					Lng: order.DropoffLon,
				})
			case OrderDelivered:
				// Set driver back to available with random movement
				nycDriverManager.MarkAvailable(req.DriverID)
			case OrderCancelled:
				nycDriverManager.MarkAvailable(req.DriverID)
			}
		}
	}

	// Broadcast status update via WebSocket/SSE
	event, _ := json.Marshal(map[string]interface{}{
		"type":    "order_status",
		"order_id": orderID,
		"status":  req.Status,
		"driver_id": req.DriverID,
	})
	if req.DriverID != "" {
		wsHub.BroadcastToDriver(req.DriverID, event)
	}
	sseBroker.Broadcast("order_status", string(event))

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"updated"}`))
}

// GET /api/stats — operational stats
func handleStats(w http.ResponseWriter, r *http.Request) {
	all := orderStore.ListAll()
	var pending, assigned, delivered int
	for _, o := range all {
		switch o.Status {
		case OrderPending:
			pending++
		case OrderAssigned:
			assigned++
		case OrderDelivered:
			delivered++
		}
	}
	var queueDepth int
	if db != nil {
		_ = db.QueryRow("SELECT COUNT(1) FROM queue WHERE status IN ('pending','processing')").Scan(&queueDepth)
	}
	resp, _ := json.Marshal(map[string]interface{}{
		"orders_total":    len(all),
		"orders_pending":  pending,
		"orders_assigned": assigned,
		"orders_delivered": delivered,
		"queue_depth":     queueDepth,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

// ─── Dispatch helpers ───────────────────────────────────────────────────────

func estimateRoute(lat1, lon1, lat2, lon2 float64) (distM, durSec float64) {
	// Haversine fallback; OSRM would be more accurate
	const R = 6371000.0
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*math.Pi/180.0)*math.Cos(lat2*math.Pi/180.0)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	distM = R * c
	durSec = distM / 8.0 // assume ~8 m/s (~29 km/h) average urban speed
	return
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	dist, _ := estimateRoute(lat1, lon1, lat2, lon2)
	return dist
}

var rustHTTP = &http.Client{Timeout: 3 * time.Second}

func queryRustNearby(lat, lon float64, radiusM float64) []string {
	const metersPerDeg = 111320.0
	radiusDeg := radiusM / metersPerDeg
	url := fmt.Sprintf("http://127.0.0.1:3030/nearby?lat=%f&lon=%f&radius_deg=%f", lat, lon, radiusDeg)
	resp, err := rustHTTP.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Count   int `json:"count"`
		Drivers []struct {
			ID string `json:"id"`
		} `json:"drivers"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	out := make([]string, 0, result.Count)
	for _, d := range result.Drivers {
		out = append(out, d.ID)
	}
	return out
}

// ─── Auth middleware ─────────────────────────────────────────────────────────

var apiKeys []string

func initAuth() {
	keys := os.Getenv("API_KEYS")
	if keys != "" {
		for _, k := range strings.Split(keys, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				apiKeys = append(apiKeys, k)
			}
		}
	}
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health/metrics
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		if len(apiKeys) > 0 {
			key := r.Header.Get("X-API-Key")
			valid := false
			for _, k := range apiKeys {
				if subtle.ConstantTimeCompare([]byte(key), []byte(k)) == 1 {
					valid = true
					break
				}
			}
			if !valid {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ─── Rate limiter middleware ─────────────────────────────────────────────────

type IPRateLimiter struct {
	mu       sync.Mutex
	clients  map[string]*rate.Limiter
	rate     rate.Limit
	burst    int
	cleanupI time.Duration
}

func NewIPRateLimiter(r rate.Limit, burst int) *IPRateLimiter {
	rl := &IPRateLimiter{
		clients:  make(map[string]*rate.Limiter),
		rate:     r,
		burst:    burst,
		cleanupI: time.Minute,
	}
	go rl.cleanup()
	return rl
}

func (l *IPRateLimiter) cleanup() {
	for {
		time.Sleep(l.cleanupI)
		l.mu.Lock()
		for ip, lim := range l.clients {
			if lim.Tokens() >= float64(l.burst) {
				delete(l.clients, ip)
			}
		}
		l.mu.Unlock()
	}
}

func (l *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim, ok := l.clients[ip]
	if !ok {
		lim = rate.NewLimiter(l.rate, l.burst)
		l.clients[ip] = lim
	}
	return lim
}

func rateLimitMiddleware(next http.Handler, limiter *IPRateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !limiter.GetLimiter(ip).Allow() {
			http.Error(w, `{"error":"rate_limit_exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─── Metrics ─────────────────────────────────────────────────────────────────

var (
	promOrdersCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "logistics_orders_created_total",
		Help: "Total orders created",
	})
	promOrdersDispatched = promauto.NewCounter(prometheus.CounterOpts{
		Name: "logistics_orders_dispatched_total",
		Help: "Total orders dispatched",
	})
)

// ─── WebSocket handler (upgrade HTTP to WS) ──────────────────────────────────

func handleWSDriver(w http.ResponseWriter, r *http.Request) {
	driverID := r.URL.Query().Get("driver_id")
	if driverID == "" {
		http.Error(w, `{"error":"driver_id required"}`, http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}

	client := &WSClient{
		ID:       fmt.Sprintf("ws-%d", time.Now().UnixNano()),
		DriverID: driverID,
		conn:     conn,
		done:     make(chan struct{}),
	}
	wsHub.Register(client)

	// Read pump: handle incoming messages (pings, status updates)
	go func() {
		defer func() {
			wsHub.Unregister(driverID)
			conn.Close()
		}()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// Handle incoming messages from driver app
			var evt struct {
				Type    string `json:"type"`
				OrderID string `json:"order_id,omitempty"`
				Status  string `json:"status,omitempty"`
			}
			if json.Unmarshal(msg, &evt) == nil && evt.Type == "status_update" && evt.OrderID != "" {
				// Driver app reports status change
				orderStore.UpdateStatus(evt.OrderID, OrderStatus(evt.Status), driverID)
				// Update driver destination if needed
				if os.Getenv("NYC_DEMO") == "1" && nycDriverManager != nil {
					order := orderStore.Get(evt.OrderID)
					if order != nil {
						switch OrderStatus(evt.Status) {
						case OrderPickedUp:
							nycDriverManager.AssignDestination(driverID, &Destination{
								Lat: order.DropoffLat,
								Lng: order.DropoffLon,
							})
						case OrderDelivered, OrderCancelled:
							nycDriverManager.MarkAvailable(driverID)
						}
					}
				}
				// Acknowledge
				ack, _ := json.Marshal(map[string]string{
					"type":    "ack",
					"order_id": evt.OrderID,
					"status":  evt.Status,
				})
				client.mu.Lock()
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				conn.WriteMessage(websocket.TextMessage, ack)
				client.mu.Unlock()
			}
		}
	}()
}

func initLogistics() {
	initAuth()
	rateLimiter := NewIPRateLimiter(100, 200) // 100 req/s, burst 200

	mux := http.NewServeMux()
	mux.Handle("/api/orders", authMiddleware(rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateOrder(w, r)
		case http.MethodGet:
			handleListOrders(w, r)
		default:
			http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		}
	}), rateLimiter)))
	mux.Handle("/api/orders/", authMiddleware(rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/status") && r.Method == http.MethodPut {
			handleUpdateOrderStatus(w, r)
			return
		}
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
	}), rateLimiter)))
	mux.Handle("/api/dispatch", authMiddleware(rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handleDispatch(w, r)
			return
		}
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
	}), rateLimiter)))
	mux.Handle("/api/stats", authMiddleware(rateLimitMiddleware(http.HandlerFunc(handleStats), rateLimiter)))

	// WebSocket endpoint for driver app (no auth via WS, driver_id in query)
	mux.HandleFunc("/ws/driver", handleWSDriver)

	// SSE endpoint for web client fallback
	mux.HandleFunc("/events", handleSSE)

	// Start API server on port 8082 (separate from gateway 8080 and route-engine 8081)
	go func() {
		addr := ":8082"
		log.Println("Logistics API running on http://127.0.0.1" + addr)
		// TLS support: if TLS_CERT and TLS_KEY env vars are set, serve HTTPS
		cert := os.Getenv("TLS_CERT")
		key := os.Getenv("TLS_KEY")
		if cert != "" && key != "" {
			log.Fatal(http.ListenAndServeTLS(addr, cert, key, mux))
		} else {
			log.Fatal(http.ListenAndServe(addr, mux))
		}
	}()
}
