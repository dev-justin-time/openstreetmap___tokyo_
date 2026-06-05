package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ─── Simulated GPS around NYC ───────────────────────────────────────────────

var nycLAT = 40.7128
var nycLON = -74.0060

func randomNYCLocation() (float64, float64) {
	// ~50km radius around NYC center
	angle := rand.Float64() * 2 * math.Pi
	dist := rand.Float64() * 0.45 // degrees ≈ 50km
	return nycLAT + dist*math.Cos(angle), nycLON + dist*math.Sin(angle)
}

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile)

	// Config from env
	driverID := os.Getenv("DRIVER_ID")
	if driverID == "" {
		driverID = fmt.Sprintf("driver-%d", rand.Intn(9000)+1000)
	}
	rustBase := os.Getenv("RUST_API")
	if rustBase == "" {
		rustBase = "http://127.0.0.1:3030"
	}
	goBase := os.Getenv("GO_API")
	if goBase == "" {
		goBase = "http://127.0.0.1:8082"
	}
	wsURL := os.Getenv("WS_URL")
	if wsURL == "" {
		wsURL = "ws://127.0.0.1:8082/ws/driver"
	}
	apiKey := os.Getenv("API_KEY")
	storeDir := os.Getenv("STORE_DIR")
	if storeDir == "" {
		storeDir = "driver_data"
	}

	// Init local store
	store, err := NewLocalStore(storeDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer store.Close()

	// Init backend client
	client := NewBackendClient(driverID, rustBase, goBase, apiKey)

	// Start position
	lat, lon := randomNYCLocation()
	client.SetPosition(lat, lon)

	// Track previous status for change detection
	var prevStatus string

	// ─── GPS update loop ─────────────────────────────────────────────────
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		// Simulate small movement
		for range ticker.C {
			lat, lon := client.Position()
			// Random walk ~50m per tick
			lat += (rand.Float64() - 0.5) * 0.0005
			lon += (rand.Float64() - 0.5) * 0.0005
			client.SetPosition(lat, lon)

			status := client.Status()
			if err := client.TrackGPS(lat, lon, status); err != nil {
				log.Printf("[gps] track error: %v", err)
			}
		}
	}()

	// ─── Order polling loop ──────────────────────────────────────────────
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			orders, err := client.FetchOrders()
			if err != nil {
				continue
			}
			for _, o := range orders {
				orderID, _ := o["id"].(string)
				if orderID == "" {
					continue
				}
				// Save locally
				lo := LocalOrder{
					ID: orderID,
					Status: getStr(o, "status"),
					PickupLat:  getFloat(o, "pickup_lat"),
					PickupLon:  getFloat(o, "pickup_lon"),
					DropoffLat: getFloat(o, "dropoff_lat"),
					DropoffLon: getFloat(o, "dropoff_lon"),
					CreatedAt:  getStr(o, "created_at"),
					UpdatedAt:  getStr(o, "updated_at"),
					Synced:     true,
				}
				if d := o["assigned_driver_id"]; d != nil {
					lo.AssignedDriverID = fmt.Sprintf("%v", d)
				}
				store.SaveOrder(lo)
			}
		}
	}()

	// ─── Try WebSocket connect ──────────────────────────────────────────
	go func() {
		// Retry loop
		for i := 0; i < 3; i++ {
			err := client.ConnectWebSocket(wsURL, func(ev DispatchEvent) {
				fmt.Printf("\n[!] DISPATCH: %s\n", ev.Message)
				switch ev.Type {
				case "order_assigned":
					lo := LocalOrder{
						ID:        ev.OrderID,
						Status:    "assigned",
						PickupLat: ev.PickupLat,
						PickupLon: ev.PickupLon,
						DropoffLat: ev.DropoffLat,
						DropoffLon: ev.DropoffLon,
						CreatedAt: time.Now().UTC().Format(time.RFC3339),
						UpdatedAt: time.Now().UTC().Format(time.RFC3339),
						Synced:    false,
					}
					store.SaveOrder(lo)
				}
			})
			if err == nil {
				fmt.Printf("[ws] connected to %s\n", wsURL)
				return
			}
			log.Printf("[ws] connect attempt %d: %v", i+1, err)
			time.Sleep(5 * time.Second)
		}
		fmt.Println("[ws] not connected — using HTTP polling")
	}()

	// ─── Sync unsynced orders ───────────────────────────────────────────
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		// Also run immediately
		for range ticker.C {
			unsynced, err := store.GetUnsyncedOrders()
			if err != nil {
				continue
			}
			for _, o := range unsynced {
				if o.Status == "assigned" || o.Status == "picked_up" {
					if err := client.UpdateOrderStatus(o.ID, o.Status); err == nil {
						store.MarkSynced(o.ID)
						fmt.Printf("[sync] order %s → %s\n", o.ID, o.Status)
					}
				}
			}
		}
	}()

	// ─── Terminal UI ────────────────────────────────────────────────────
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("  NYC Driver App  |  ID: %s\n", driverID)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("Commands:")
	fmt.Println("  status [available|busy]   — set driver status")
	fmt.Println("  orders                    — list pending orders")
	fmt.Println("  accept <order_id>         — accept order")
	fmt.Println("  pickup <order_id>         — mark order picked up")
	fmt.Println("  deliver <order_id>        — mark order delivered")
	fmt.Println("  locate                    — show current position")
	fmt.Println("  quit                      — exit")
	fmt.Println(strings.Repeat("-", 60))

	scanner := bufio.NewScanner(os.Stdin)

	// Status display ticker
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			lat, lon := client.Position()
			status := client.Status()
			if status != prevStatus {
				fmt.Printf("\n[driver] %s | status: %s | (%.4f, %.4f)\n", driverID, status, lat, lon)
				prevStatus = status
			}
		}
	}()

	// Signal handler
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n[driver] shutting down...")
		client.DisconnectWebSocket()
		os.Exit(0)
	}()

	// Command loop
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
			fmt.Println("bye")
			client.DisconnectWebSocket()
			return

		case "status":
			if len(args) == 0 {
				fmt.Printf("current status: %s\n", client.Status())
				continue
			}
			newStatus := args[0]
			if newStatus != "available" && newStatus != "busy" {
				fmt.Println("usage: status [available|busy]")
				continue
			}
			client.SetStatus(newStatus)
			lat, lon := client.Position()
			client.TrackGPS(lat, lon, newStatus)
			fmt.Printf("status → %s\n", newStatus)

		case "orders":
			orders, err := store.ListPendingOrders()
			if err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			if len(orders) == 0 {
				fmt.Println("no pending orders")
				continue
			}
			fmt.Println(strings.Repeat("-", 60))
			for _, o := range orders {
				syncMark := " "
				if !o.Synced {
					syncMark = "!"
				}
				fmt.Printf("[%s] %s %s\n", o.Status[:3], o.ID, syncMark)
				fmt.Printf("      pickup: (%.4f, %.4f)\n", o.PickupLat, o.PickupLon)
				fmt.Printf("      drop:   (%.4f, %.4f)\n", o.DropoffLat, o.DropoffLon)
				d := haversine(o.PickupLat, o.PickupLon, o.DropoffLat, o.DropoffLon)
				fmt.Printf("      dist:   %.1f km\n", d)
			}
			fmt.Println(strings.Repeat("-", 60))

		case "accept", "pickup", "deliver":
			if len(args) < 1 {
				fmt.Printf("usage: %s <order_id>\n", cmd)
				continue
			}
			orderID := args[0]
			var newStatus string
			switch cmd {
			case "accept":
				newStatus = "assigned"
			case "pickup":
				newStatus = "picked_up"
			case "deliver":
				newStatus = "delivered"
			}

			// Update locally first
			store.UpdateOrderStatus(orderID, newStatus)
			if err := client.UpdateOrderStatus(orderID, newStatus); err != nil {
				fmt.Printf("[warn] remote update failed: %v (saved locally)\n", err)
				// Mark unsynced for retry
				if o, err := store.GetOrder(orderID); err == nil {
					o.Synced = false
					store.SaveOrder(*o)
				}
			} else {
				store.MarkSynced(orderID)
			}
			fmt.Printf("order %s → %s\n", orderID, newStatus)

		case "locate":
			lat, lon := client.Position()
			fmt.Printf("position: (%.4f, %.4f)\n", lat, lon)
			fmt.Printf("  maps: https://www.openstreetmap.org/?mlat=%.4f&mlon=%.4f\n", lat, lon)

		default:
			fmt.Printf("unknown command: %s\n", cmd)
		}
	}
}

func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func getFloat(m map[string]interface{}, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return 6371.0 * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
