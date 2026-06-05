package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type BackendClient struct {
	rustBase    string
	goBase      string
	apiKey      string
	driverID    string
	lat, lon    atomic.Value // float64
	status      atomic.Value // string
	wsConn      *websocket.Conn
	httpClient  *http.Client
	onDispatch  func(DispatchEvent)
	connected   atomic.Bool
}

type DispatchEvent struct {
	Type    string `json:"type"`
	OrderID string `json:"order_id,omitempty"`
	DriverID string `json:"driver_id,omitempty"`
	PickupLat float64 `json:"pickup_lat,omitempty"`
	PickupLon float64 `json:"pickup_lon,omitempty"`
	DropoffLat float64 `json:"dropoff_lat,omitempty"`
	DropoffLon float64 `json:"dropoff_lon,omitempty"`
	Message string `json:"message,omitempty"`
}

func NewBackendClient(driverID, rustBase, goBase, apiKey string) *BackendClient {
	c := &BackendClient{
		driverID:   driverID,
		rustBase:   rustBase,
		goBase:     goBase,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	c.lat.Store(0.0)
	c.lon.Store(0.0)
	c.status.Store("available")
	return c
}

func (c *BackendClient) SetPosition(lat, lon float64) {
	c.lat.Store(lat)
	c.lon.Store(lon)
}

func (c *BackendClient) Position() (float64, float64) {
	return c.lat.Load().(float64), c.lon.Load().(float64)
}

func (c *BackendClient) SetStatus(s string) {
	c.status.Store(s)
}

func (c *BackendClient) Status() string {
	return c.status.Load().(string)
}

func (c *BackendClient) IsConnected() bool {
	return c.connected.Load()
}

// TrackGPS sends a GPS update to the Rust tracker.
func (c *BackendClient) TrackGPS(lat, lon float64, status string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"id":     c.driverID,
		"lat":    lat,
		"lon":    lon,
		"status": status,
	})
	req, err := http.NewRequest("POST", c.rustBase+"/track", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// FetchOrders gets assigned orders from the Go logistics API.
func (c *BackendClient) FetchOrders() ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/orders?status=assigned", c.goBase)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Orders []map[string]interface{} `json:"orders"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.Orders, nil
}

// UpdateOrderStatus sends a status update to the Go logistics API.
func (c *BackendClient) UpdateOrderStatus(orderID, status string) error {
	body, _ := json.Marshal(map[string]string{"id": orderID, "new_status": status})
	url := fmt.Sprintf("%s/api/orders/%s/status", c.goBase, orderID)
	req, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

// ConnectWebSocket connects to the Go logistics WebSocket for real-time dispatch.
func (c *BackendClient) ConnectWebSocket(wsURL string, onDispatch func(DispatchEvent)) error {
	c.onDispatch = onDispatch

	dialer := websocket.DefaultDialer
	header := http.Header{}
	if c.apiKey != "" {
		header.Set("X-API-Key", c.apiKey)
	}

	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	c.wsConn = conn

	// Send registration
	reg, _ := json.Marshal(map[string]string{"type": "register", "driver_id": c.driverID})
	if err := conn.WriteMessage(websocket.TextMessage, reg); err != nil {
		conn.Close()
		return fmt.Errorf("ws register: %w", err)
	}

	c.connected.Store(true)

	// Read loop
	go func() {
		defer func() {
			c.connected.Store(false)
		}()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("[ws] read error: %v", err)
				return
			}
			var ev DispatchEvent
			if err := json.Unmarshal(msg, &ev); err != nil {
				log.Printf("[ws] unmarshal error: %v", err)
				continue
			}
			if c.onDispatch != nil {
				c.onDispatch(ev)
			}
		}
	}()

	// Ping ticker
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if !c.connected.Load() {
				return
			}
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[ws] ping error: %v", err)
				return
			}
		}
	}()

	return nil
}

// DisconnectWebSocket closes the WebSocket connection.
func (c *BackendClient) DisconnectWebSocket() {
	if c.wsConn != nil {
		c.wsConn.Close()
	}
	c.connected.Store(false)
}
