package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ─── Types ────────────────────────────────────────────────────────────────────

type Destination struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type Driver struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Lat         float64      `json:"lat"`
	Lng         float64      `json:"lng"`
	Heading     float64      `json:"heading"`
	Speed       float64      `json:"speed"`
	Status      string       `json:"status"`
	Destination *Destination `json:"destination,omitempty"`
	LastUpdate  time.Time    `json:"last_update"`
}

type DriverManager struct {
	drivers map[string]*Driver
	mu      sync.RWMutex
	rnd     *rand.Rand
}

// ─── DestinationDB ────────────────────────────────────────────────────────────

type DestinationDB struct {
	dests []Destination
	rnd   *rand.Rand
}

func NewDestinationDB() *DestinationDB {
	return &DestinationDB{
		rnd: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (db *DestinationDB) Initialize() error {
	landmarks := []struct {
		Name string
		Lat  float64
		Lon  float64
	}{
		{"Times Square", 40.7580, -73.9855},
		{"Central Park", 40.7829, -73.9654},
		{"Brooklyn Bridge", 40.7061, -73.9969},
		{"Empire State Building", 40.7484, -73.9857},
		{"Statue of Liberty", 40.6892, -74.0445},
		{"Wall Street", 40.7074, -74.0113},
		{"Madison Square Garden", 40.7505, -73.9934},
		{"Rockefeller Center", 40.7587, -73.9787},
		{"One World Trade Center", 40.7127, -74.0134},
		{"Yankee Stadium", 40.8296, -73.9262},
		{"Citi Field", 40.7571, -73.8458},
		{"JFK Airport", 40.6413, -73.7781},
		{"LaGuardia Airport", 40.7769, -73.8740},
		{"Grand Central Terminal", 40.7527, -73.9772},
		{"Union Square", 40.7360, -73.9904},
		{"Columbia University", 40.8075, -73.9626},
		{"Hudson Yards", 40.7549, -74.0020},
	}
	for _, l := range landmarks {
		db.dests = append(db.dests, Destination{Lat: l.Lat, Lng: l.Lon})
	}
	return nil
}

func (db *DestinationDB) Count() int {
	return len(db.dests)
}

func (db *DestinationDB) GetRandomDestination() *Destination {
	if len(db.dests) == 0 {
		return nil
	}
	d := db.dests[db.rnd.Intn(len(db.dests))]
	return &d
}

// ─── DriverManager ────────────────────────────────────────────────────────────

const (
	nycLatMin = 40.4913
	nycLatMax = 40.9176
	nycLngMin = -74.2591
	nycLngMax = -73.7004
)

func NewDriverManager(count int) *DriverManager {
	dm := &DriverManager{
		drivers: make(map[string]*Driver),
		rnd:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	for i := 0; i < count; i++ {
		driver := dm.generateDriver(i)
		dm.drivers[driver.ID] = driver
	}
	return dm
}

func (dm *DriverManager) generateDriver(index int) *Driver {
	firstNames := []string{"James", "Mary", "John", "Patricia", "Robert", "Jennifer", "Michael", "Linda", "David", "Elizabeth"}
	lastNames := []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Rodriguez", "Martinez"}
	return &Driver{
		ID:         fmt.Sprintf("driver_%d", index),
		Name:       fmt.Sprintf("%s %s", firstNames[dm.rnd.Intn(len(firstNames))], lastNames[dm.rnd.Intn(len(lastNames))]),
		Lat:        nycLatMin + dm.rnd.Float64()*(nycLatMax-nycLatMin),
		Lng:        nycLngMin + dm.rnd.Float64()*(nycLngMax-nycLngMin),
		Heading:    dm.rnd.Float64() * 360,
		Speed:      20 + dm.rnd.Float64()*40,
		Status:     "available",
		LastUpdate: time.Now(),
	}
}

func (dm *DriverManager) GetAllDrivers() []Driver {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	drivers := make([]Driver, 0, len(dm.drivers))
	for _, d := range dm.drivers {
		drivers = append(drivers, *d)
	}
	return drivers
}

func (dm *DriverManager) GetDriver(id string) *Driver {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	if d, ok := dm.drivers[id]; ok {
		c := *d
		return &c
	}
	return nil
}

func (dm *DriverManager) AssignDestination(driverID string, dest *Destination) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	if driver, ok := dm.drivers[driverID]; ok {
		driver.Destination = dest
		driver.Status = "en_route"
	}
}

func (dm *DriverManager) UpdatePositions() {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	for _, driver := range dm.drivers {
		if driver.Status == "en_route" && driver.Destination != nil {
			dm.moveTowardDestination(driver)
		} else {
			dm.randomMovement(driver)
		}
		driver.LastUpdate = time.Now()
	}
}

func (dm *DriverManager) moveTowardDestination(driver *Driver) {
	dest := driver.Destination
	dLat := dest.Lat - driver.Lat
	dLng := dest.Lng - driver.Lng
	dist := math.Sqrt(dLat*dLat + dLng*dLng)
	if dist < 0.0001 {
		driver.Status = "delivering"
		driver.Speed = 0
		return
	}
	heading := math.Atan2(dLng, dLat) * 180 / math.Pi
	driver.Heading = math.Mod(heading+360, 360)
	speedFactor := driver.Speed / 100000
	driver.Lat += dLat * speedFactor
	driver.Lng += dLng * speedFactor
}

func (dm *DriverManager) randomMovement(driver *Driver) {
	driver.Heading += (dm.rnd.Float64() - 0.5) * 20
	driver.Heading = math.Mod(driver.Heading+360, 360)
	radians := driver.Heading * math.Pi / 180
	speedFactor := driver.Speed / 100000
	driver.Lat += math.Cos(radians) * speedFactor
	driver.Lng += math.Sin(radians) * speedFactor
	if driver.Lat < nycLatMin || driver.Lat > nycLatMax {
		driver.Heading = 180 - driver.Heading
		driver.Lat = math.Max(nycLatMin, math.Min(nycLatMax, driver.Lat))
	}
	if driver.Lng < nycLngMin || driver.Lng > nycLngMax {
		driver.Heading = 360 - driver.Heading
		driver.Lng = math.Max(nycLngMin, math.Min(nycLngMax, driver.Lng))
	}
}

// ─── WebSocket Server ────────────────────────────────────────────────────────

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type DriverUpdate struct {
	Type    string   `json:"type"`
	Drivers []Driver `json:"drivers,omitempty"`
	Driver  *Driver  `json:"driver,omitempty"`
}

type Server struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan DriverUpdate
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
	drivers    *DriverManager
	destDB     *DestinationDB
}

func NewServer() *Server {
	s := &Server{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan DriverUpdate, 100),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		drivers:    NewDriverManager(1000),
		destDB:     NewDestinationDB(),
	}
	go s.run()
	return s
}

func (s *Server) run() {
	for {
		select {
		case conn := <-s.register:
			s.mu.Lock()
			s.clients[conn] = true
			s.mu.Unlock()
			s.sendInitialState(conn)
		case conn := <-s.unregister:
			s.mu.Lock()
			if _, ok := s.clients[conn]; ok {
				delete(s.clients, conn)
				conn.Close()
			}
			s.mu.Unlock()
		case update := <-s.broadcast:
			s.mu.RLock()
			for conn := range s.clients {
				err := conn.WriteJSON(update)
				if err != nil {
					conn.Close()
					delete(s.clients, conn)
				}
			}
			s.mu.RUnlock()
		}
	}
}

func (s *Server) sendInitialState(conn *websocket.Conn) {
	drivers := s.drivers.GetAllDrivers()
	conn.WriteJSON(DriverUpdate{Type: "init", Drivers: drivers})
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}
	s.register <- conn
	defer func() { s.unregister <- conn }()
	for {
		var msg map[string]interface{}
		err := conn.ReadJSON(&msg)
		if err != nil {
			break
		}
	}
}

func (s *Server) handleAssignOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DriverID string `json:"driver_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	driver := s.drivers.GetDriver(req.DriverID)
	if driver == nil {
		http.Error(w, "Driver not found", http.StatusNotFound)
		return
	}
	dest := s.destDB.GetRandomDestination()
	if dest == nil {
		http.Error(w, "No destinations available", http.StatusInternalServerError)
		return
	}
	s.drivers.AssignDestination(req.DriverID, dest)
	s.broadcast <- DriverUpdate{Type: "assignment", Driver: driver}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"driver":      driver,
		"destination": dest,
	})
}

func (s *Server) handleNYCStats(w http.ResponseWriter, r *http.Request) {
	drivers := s.drivers.GetAllDrivers()
	avail, enRoute, delivering := 0, 0, 0
	for _, d := range drivers {
		switch d.Status {
		case "available":
			avail++
		case "en_route":
			enRoute++
		case "delivering":
			delivering++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total":      len(drivers),
		"available":  avail,
		"en_route":   enRoute,
		"delivering": delivering,
	})
}

func (s *Server) handleListDrivers(w http.ResponseWriter, r *http.Request) {
	drivers := s.drivers.GetAllDrivers()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":   len(drivers),
		"drivers": drivers,
	})
}

func (s *Server) startDriverSimulation() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		s.drivers.UpdatePositions()
		drivers := s.drivers.GetAllDrivers()
		s.broadcast <- DriverUpdate{Type: "update", Drivers: drivers}
	}
}

// ─── NVIDIA API Proxy ──────────────────────────────────────────────────────────

func handleNVIDIAProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Use frontend-provided key first, fall back to env var
	apiKey := r.Header.Get("X-NVIDIA-Key")
	if apiKey == "" {
		apiKey = os.Getenv("NVIDIA_API_KEY")
	}
	if apiKey == "" {
		http.Error(w, `{"error":"NVIDIA_API_KEY not set"}`, http.StatusInternalServerError)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read error"}`, http.StatusBadRequest)
		return
	}

	nvidiaURL := "https://integrate.api.nvidia.com/v1/chat/completions"
	req, err := http.NewRequest("POST", nvidiaURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, `{"error":"create request failed"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Handle NVIDIA async pattern: POST returns 202 with Location header
	if resp.StatusCode == http.StatusAccepted {
		location := resp.Header.Get("Location")
		if location == "" {
			http.Error(w, `{"error":"202 without Location header"}`, http.StatusBadGateway)
			return
		}
		pollReq, _ := http.NewRequest("GET", location, nil)
		pollReq.Header.Set("Authorization", "Bearer "+apiKey)
		pollClient := &http.Client{Timeout: 120 * time.Second}
		for i := 0; i < 120; i++ {
			time.Sleep(500 * time.Millisecond)
			pollResp, err := pollClient.Do(pollReq)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"poll error: %s"}`, err.Error()), http.StatusBadGateway)
				return
			}
			if pollResp.StatusCode == http.StatusOK {
				pollBody, _ := io.ReadAll(pollResp.Body)
				pollResp.Body.Close()
				w.Header().Set("Content-Type", "application/json")
				w.Write(pollBody)
				return
			}
			pollResp.Body.Close()
		}
		http.Error(w, `{"error":"poll timeout"}`, http.StatusGatewayTimeout)
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, `{"error":"read response failed"}`, http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// ─── CORS Middleware ───────────────────────────────────────────────────────────

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-NVIDIA-Key")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	server := NewServer()
	if err := server.destDB.Initialize(); err != nil {
		log.Fatal("Failed to initialize destination DB:", err)
	}
	log.Printf("Loaded %d NYC destinations", server.destDB.Count())
	go server.startDriverSimulation()
	http.Handle("/", cors(http.FileServer(http.Dir("../../frontend"))))
	http.Handle("/tiles/", cors(http.StripPrefix("/tiles/", http.FileServer(http.Dir("../plateau-server/plateau_3dtiles")))))
	http.HandleFunc("/ws", server.handleWebSocket)
	http.HandleFunc("/api/assign", cors(http.HandlerFunc(server.handleAssignOrder)).ServeHTTP)
	http.HandleFunc("/api/stats", cors(http.HandlerFunc(server.handleNYCStats)).ServeHTTP)
	http.HandleFunc("/api/drivers", cors(http.HandlerFunc(server.handleListDrivers)).ServeHTTP)
	http.HandleFunc("/api/nvidia/chat", cors(http.HandlerFunc(handleNVIDIAProxy)).ServeHTTP)
	log.Println("NYC Driver App starting on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
