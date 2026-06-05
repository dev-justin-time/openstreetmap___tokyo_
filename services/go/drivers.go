package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// NYC bounding box
const (
	NYCLatMin = 40.4913
	NYCLatMax = 40.9176
	NYCLngMin = -74.2591
	NYCLngMax = -73.7004
)

type Driver struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Lat         float64      `json:"lat"`
	Lng         float64      `json:"lng"`
	Heading     float64      `json:"heading"`
	Speed       float64      `json:"speed"`
	Status      string       `json:"status"` // "available", "en_route", "delivering"
	Destination *Destination `json:"destination,omitempty"`
	LastUpdate  time.Time    `json:"last_update"`
}

type DriverManager struct {
	drivers map[string]*Driver
	mu      sync.RWMutex
	rand    *rand.Rand
}

func NewDriverManager(count int) *DriverManager {
	dm := &DriverManager{
		drivers: make(map[string]*Driver),
		rand:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	// Generate random drivers
	for i := 0; i < count; i++ {
		driver := dm.generateDriver(i)
		dm.drivers[driver.ID] = driver
	}

	return dm
}

func (dm *DriverManager) generateDriver(index int) *Driver {
	firstNames := []string{"James", "Mary", "John", "Patricia", "Robert", "Jennifer", "Michael", "Linda", "David", "Elizabeth"}
	lastNames := []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Rodriguez", "Martinez"}

	firstName := firstNames[dm.rand.Intn(len(firstNames))]
	lastName := lastNames[dm.rand.Intn(len(lastNames))]

	return &Driver{
		ID:         fmt.Sprintf("driver_%d", index),
		Name:       fmt.Sprintf("%s %s", firstName, lastName),
		Lat:        dm.randomLat(),
		Lng:        dm.randomLng(),
		Heading:    dm.rand.Float64() * 360,
		Speed:      20 + dm.rand.Float64()*40, // 20-60 mph
		Status:     "available",
		LastUpdate: time.Now(),
	}
}

func (dm *DriverManager) randomLat() float64 {
	return NYCLatMin + dm.rand.Float64()*(NYCLatMax-NYCLatMin)
}

func (dm *DriverManager) randomLng() float64 {
	return NYCLngMin + dm.rand.Float64()*(NYCLngMax-NYCLngMin)
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
		driverCopy := *d
		return &driverCopy
	}
	return nil
}

func (dm *DriverManager) UpdatePositions() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	for _, driver := range dm.drivers {
		if driver.Status == "en_route" && driver.Destination != nil {
			// Move toward destination
			dm.moveTowardDestination(driver)
		} else {
			// Random movement
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

	if dist < 0.0001 { // Very close to destination
		driver.Status = "delivering"
		driver.Speed = 0
		return
	}

	// Calculate heading
	heading := math.Atan2(dLng, dLat) * 180 / math.Pi
	driver.Heading = math.Mod(heading+360, 360)

	// Move toward destination (speed in degrees per update)
	speedFactor := driver.Speed / 100000 // Convert mph to degree movement
	driver.Lat += dLat * speedFactor
	driver.Lng += dLng * speedFactor
}

func (dm *DriverManager) randomMovement(driver *Driver) {
	// Slight random heading change
	driver.Heading += (dm.rand.Float64() - 0.5) * 20
	driver.Heading = math.Mod(driver.Heading+360, 360)

	// Move in current direction
	radians := driver.Heading * math.Pi / 180
	speedFactor := driver.Speed / 100000

	driver.Lat += math.Cos(radians) * speedFactor
	driver.Lng += math.Sin(radians) * speedFactor

	// Keep within NYC bounds
	if driver.Lat < NYCLatMin || driver.Lat > NYCLatMax {
		driver.Heading = 180 - driver.Heading
		driver.Lat = math.Max(NYCLatMin, math.Min(NYCLatMax, driver.Lat))
	}
	if driver.Lng < NYCLngMin || driver.Lng > NYCLngMax {
		driver.Heading = 360 - driver.Heading
		driver.Lng = math.Max(NYCLngMin, math.Min(NYCLngMax, driver.Lng))
	}
}

func (dm *DriverManager) AssignDestination(driverID string, dest *Destination) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if driver, ok := dm.drivers[driverID]; ok {
		driver.Destination = dest
		driver.Status = "en_route"
	}
}

func (dm *DriverManager) MarkAvailable(driverID string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if driver, ok := dm.drivers[driverID]; ok {
		driver.Destination = nil
		driver.Status = "available"
		driver.Speed = 20 + dm.rand.Float64()*40
	}
}

// ─── HTTP handlers for NYC DriverManager ────────────────────────────────────

var nycDriverManager *DriverManager

func startNYCSimulator(rustAddrs []string) {
	count := 500 // default
	if v := os.Getenv("NYC_DRIVER_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			count = n
		}
	}
	nycDriverManager = NewDriverManager(count)
	log.Printf("[nyc] NYC DriverManager started with %d drivers", count)

	rustURL := rustAddrs[0]

	// Push all drivers to Rust on startup
	for _, d := range nycDriverManager.GetAllDrivers() {
		pushDriverToRust(rustURL, &d)
	}

	// Update positions every 2 seconds and push to Rust
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		nycDriverManager.UpdatePositions()
		for _, d := range nycDriverManager.GetAllDrivers() {
			pushDriverToRust(rustURL, &d)
		}
	}
}

func pushDriverToRust(rustURL string, d *Driver) {
	body, _ := json.Marshal(map[string]interface{}{
		"id":     d.ID,
		"lat":    d.Lat,
		"lon":    d.Lng,
		"status": d.Status,
	})
	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest("POST", rustURL+"/track", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func handleNYCDrivers(w http.ResponseWriter, r *http.Request) {
	if nycDriverManager == nil {
		http.Error(w, `{"error":"nyc not initialized"}`, http.StatusServiceUnavailable)
		return
	}
	drivers := nycDriverManager.GetAllDrivers()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":   len(drivers),
		"drivers": drivers,
	})
}

func handleNYCStats(w http.ResponseWriter, r *http.Request) {
	if nycDriverManager == nil {
		http.Error(w, `{"error":"nyc not initialized"}`, http.StatusServiceUnavailable)
		return
	}
	drivers := nycDriverManager.GetAllDrivers()
	avail := 0
	enRoute := 0
	delivering := 0
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
