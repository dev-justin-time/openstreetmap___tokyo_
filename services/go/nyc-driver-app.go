module nyc-driver-app

go 1.21

require (
	github.com/gorilla/websocket v1.5.1
	github.com/mattn/go-sqlite3 v1.14.22
)

require golang.org/x/net v0.17.0 // indirect




package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
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

type DriverUpdate struct {
	Type    string   `json:"type"`
	Drivers []Driver `json:"drivers,omitempty"`
	Driver  *Driver  `json:"driver,omitempty"`
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
			// Send current driver state to new client
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
	update := DriverUpdate{
		Type:    "init",
		Drivers: drivers,
	}
	conn.WriteJSON(update)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}
	s.register <- conn

	defer func() {
		s.unregister <- conn
	}()

	for {
		var msg map[string]interface{}
		err := conn.ReadJSON(&msg)
		if err != nil {
			break
		}
		// Handle client messages if needed
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

	// Get random destination
	dest := s.destDB.GetRandomDestination()
	if dest == nil {
		http.Error(w, "No destinations available", http.StatusInternalServerError)
		return
	}

	// Assign destination to driver
	s.drivers.AssignDestination(req.DriverID, dest)

	// Broadcast update
	s.broadcast <- DriverUpdate{
		Type:   "assignment",
		Driver: driver,
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"driver":      driver,
		"destination": dest,
	})
}

func (s *Server) startDriverSimulation() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		// Update driver positions
		s.drivers.UpdatePositions()

		// Broadcast updates
		drivers := s.drivers.GetAllDrivers()
		s.broadcast <- DriverUpdate{
			Type:    "update",
			Drivers: drivers,
		}
	}
}

func main() {
	server := NewServer()

	// Initialize destination database
	if err := server.destDB.Initialize(); err != nil {
		log.Fatal("Failed to initialize destination DB:", err)
	}
	log.Printf("Loaded %d NYC destinations", server.destDB.Count())

	// Start driver simulation
	go server.startDriverSimulation()

	// Serve frontend
	http.Handle("/", http.FileServer(http.Dir("frontend")))

	// WebSocket endpoint
	http.HandleFunc("/ws", server.handleWebSocket)

	// API endpoints
	http.HandleFunc("/api/assign", server.handleAssignOrder)

	log.Println("🚗 NYC Driver App starting on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}