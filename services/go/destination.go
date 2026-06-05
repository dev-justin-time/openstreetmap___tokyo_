package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ─── Destination type (used by drivers.go) ──────────────────────────────────

type Destination struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// ─── NYC landmarks for demo destinations ────────────────────────────────────

var nycDestinations = []struct {
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
	{"Washington Square Park", 40.7308, -73.9973},
	{"Columbia University", 40.8075, -73.9626},
	{"Brooklyn Navy Yard", 40.7025, -73.9700},
	{"Hudson Yards", 40.7549, -74.0020},
	{"South Street Seaport", 40.7075, -74.0035},
}

// ─── Demo coordinator ───────────────────────────────────────────────────────

type DemoCoordinator struct {
	rustBase   string
	goBase     string
	apiKey     string
	driverIDs  []string
	httpClient *http.Client
}

func NewDemoCoordinator(rustBase, goBase, apiKey string) *DemoCoordinator {
	return &DemoCoordinator{
		rustBase:   rustBase,
		goBase:     goBase,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// CreateOrders generates N random orders with pickup/dropoff at NYC landmarks.
func (d *DemoCoordinator) CreateOrders(n int) []map[string]interface{} {
	orders := make([]map[string]interface{}, 0, n)
	for i := 0; i < n; i++ {
		// Pick two random distinct landmarks
		a := rand.Intn(len(nycDestinations))
		b := rand.Intn(len(nycDestinations))
		for b == a {
			b = rand.Intn(len(nycDestinations))
		}

		order := map[string]interface{}{
			"pickup_lat":   nycDestinations[a].Lat,
			"pickup_lon":   nycDestinations[a].Lon,
			"dropoff_lat":  nycDestinations[b].Lat,
			"dropoff_lon":  nycDestinations[b].Lon,
			"pickup_addr":  nycDestinations[a].Name,
			"dropoff_addr": nycDestinations[b].Name,
		}
		orders = append(orders, order)
	}
	return orders
}

// PostOrder sends one order to the Go logistics API.
func (d *DemoCoordinator) PostOrder(order map[string]interface{}) (string, error) {
	body, _ := json.Marshal(order)
	req, err := http.NewRequest("POST", d.goBase+"/api/orders", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.apiKey != "" {
		req.Header.Set("X-API-Key", d.apiKey)
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result struct {
		Order struct {
			ID string `json:"id"`
		} `json:"order"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse: %w body: %s", err, string(data))
	}
	return result.Order.ID, nil
}

// GenerateDemo generates a complete demo scenario with orders and driver tracking.
func (d *DemoCoordinator) GenerateDemo(numOrders int) {
	fmt.Printf("[demo] generating %d orders across %d NYC landmarks...\n",
		numOrders, len(nycDestinations))

	orders := d.CreateOrders(numOrders)
	created := 0
	for _, o := range orders {
		id, err := d.PostOrder(o)
		if err != nil {
			log.Printf("[demo] order failed: %v", err)
			continue
		}
		created++
		if created <= 3 || created == numOrders || created%10 == 0 {
			fmt.Printf("[demo]   order %s: %s → %s\n", id, o["pickup_addr"], o["dropoff_addr"])
		}
	}
	fmt.Printf("[demo] created %d/%d orders\n", created, numOrders)
}

// DispatchAll attempts to dispatch all pending orders.
func (d *DemoCoordinator) DispatchAll() {
	fmt.Println("[demo] fetching pending orders...")

	// Get pending orders from logistics API
	req, _ := http.NewRequest("GET", d.goBase+"/api/orders?status=pending", nil)
	if d.apiKey != "" {
		req.Header.Set("X-API-Key", d.apiKey)
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		log.Printf("[demo] fetch orders error: %v", err)
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result struct {
		Orders []struct {
			ID string `json:"id"`
		} `json:"orders"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		log.Printf("[demo] parse orders error: %v", err)
		return
	}

	fmt.Printf("[demo] dispatching %d pending orders...\n", len(result.Orders))
	dispatched := 0
	for _, o := range result.Orders {
		body, _ := json.Marshal(map[string]string{"order_id": o.ID})
		req, err := http.NewRequest("POST", d.goBase+"/api/dispatch", bytes.NewReader(body))
		if err != nil {
			log.Printf("[demo] dispatch req error: %v", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if d.apiKey != "" {
			req.Header.Set("X-API-Key", d.apiKey)
		}
		resp, err := d.httpClient.Do(req)
		if err != nil {
			log.Printf("[demo] dispatch error: %v", err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			dispatched++
		}
	}
	fmt.Printf("[demo] dispatched %d/%d orders\n", dispatched, len(result.Orders))
}

// SimulateDrivers moves simulated driver positions toward a destination over time.
func (d *DemoCoordinator) SimulateDrivers(count int) {
	fmt.Printf("[demo] simulating %d drivers...\n", count)
	d.driverIDs = make([]string, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("demo-driver-%d", i+1)
		d.driverIDs[i] = id
		// Start at random NYC location
		dest := nycDestinations[rand.Intn(len(nycDestinations))]
		lat := dest.Lat + (rand.Float64()-0.5)*0.02
		lon := dest.Lon + (rand.Float64()-0.5)*0.02
		// Register with Rust tracker
		d.trackDriver(id, lat, lon, "available")
	}
	fmt.Printf("[demo] %d drivers registered\n", count)
}

func (d *DemoCoordinator) trackDriver(id string, lat, lon float64, status string) {
	body, _ := json.Marshal(map[string]interface{}{
		"id": id, "lat": lat, "lon": lon, "status": status,
	})
	req, _ := http.NewRequest("POST", d.rustBase+"/track", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if d.apiKey != "" {
		req.Header.Set("X-API-Key", d.apiKey)
	}
	resp, err := d.httpClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// RunContinuousDemo runs a continuous demo generating orders and simulating drivers.
func (d *DemoCoordinator) RunContinuousDemo(interval time.Duration, ordersPerCycle int) {
	fmt.Printf("[demo] continuous mode: %d orders every %v\n", ordersPerCycle, interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Generate initial batch
	d.GenerateDemo(ordersPerCycle)

	for range ticker.C {
		d.GenerateDemo(ordersPerCycle)
		d.DispatchAll()
	}
}

// ─── CLI entry point ────────────────────────────────────────────────────────

func runDemo() {
	rustBase := getEnvDefault("RUST_API", "http://127.0.0.1:3030")
	goBase := getEnvDefault("GO_API", "http://127.0.0.1:8082")
	apiKey := getEnvDefault("API_KEY", "")
	mode := getEnvDefault("DEMO_MODE", "once")
	driverCount := parseIntEnv("DEMO_DRIVERS", 50)
	orderCount := parseIntEnv("DEMO_ORDERS", 20)
	intervalSec := parseIntEnv("DEMO_INTERVAL_SEC", 60)

	coord := NewDemoCoordinator(rustBase, goBase, apiKey)

	switch mode {
	case "once":
		fmt.Println("[demo] mode=once")
		coord.SimulateDrivers(driverCount)
		time.Sleep(2 * time.Second)
		coord.GenerateDemo(orderCount)
		time.Sleep(1 * time.Second)
		coord.DispatchAll()

	case "continuous":
		fmt.Println("[demo] mode=continuous")
		coord.SimulateDrivers(driverCount)
		coord.RunContinuousDemo(time.Duration(intervalSec)*time.Second, orderCount)

	case "orders-only":
		fmt.Println("[demo] mode=orders-only")
		coord.GenerateDemo(orderCount)

	case "drivers-only":
		fmt.Println("[demo] mode=drivers-only")
		coord.SimulateDrivers(driverCount)

	default:
		// Interactive demo
		fmt.Println(strings.Repeat("=", 60))
		fmt.Println("  NYC Demo Coordinator")
		fmt.Println(strings.Repeat("=", 60))
		fmt.Println("Commands:")
		fmt.Println("  drivers N              — simulate N drivers")
		fmt.Println("  orders N               — create N random orders")
		fmt.Println("  dispatch               — dispatch pending orders")
		fmt.Println("  auto [interval_sec]    — continuous mode")
		fmt.Println("  stats                  — show system stats")
		fmt.Println("  quit                   — exit")
		fmt.Println(strings.Repeat("-", 60))

		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			cmd := parts[0]
			args := parts[1:]

			switch cmd {
			case "quit", "exit", "q":
				return
			case "drivers":
				n := 10
				if len(args) > 0 {
					n, _ = strconv.Atoi(args[0])
				}
				coord.SimulateDrivers(n)
			case "orders":
				n := 5
				if len(args) > 0 {
					n, _ = strconv.Atoi(args[0])
				}
				coord.GenerateDemo(n)
			case "dispatch":
				coord.DispatchAll()
			case "auto":
				sec := 30
				if len(args) > 0 {
					sec, _ = strconv.Atoi(args[0])
				}
				coord.RunContinuousDemo(time.Duration(sec)*time.Second, 5)
			case "stats":
				fetchAndShowStats(goBase, apiKey)
			default:
				fmt.Printf("unknown: %s\n", cmd)
			}
		}
	}
}

func fetchAndShowStats(goBase, apiKey string) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", goBase+"/api/stats", nil)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("stats error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var pretty bytes.Buffer
	json.Indent(&pretty, data, "", "  ")
	fmt.Println(pretty.String())
}

// ─── Init ───────────────────────────────────────────────────────────────────

func initDemo() {
	if os.Getenv("DEMO_ENABLED") == "1" {
		go func() {
			time.Sleep(3 * time.Second) // wait for services to start
			runDemo()
		}()
	}
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseIntEnv(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return def
}
