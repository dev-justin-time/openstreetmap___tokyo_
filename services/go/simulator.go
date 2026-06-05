package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

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
Uses rendezvous hashing to pick Rust instance for each driver id.
*/
func startSimulator(count int, interval time.Duration, rustAddrs []string) {
	centerLat := 19.4326
	centerLon := -99.1332

	type drv struct {
		id     string
		lat    float64
		lon    float64
		status string
	}
	drivers := make([]*drv, 0, count)
	for i := 0; i < count; i++ {
		offsetLat := (rand.Float64()-0.5)*0.02
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

	scoreFor := func(id string, addr string) uint64 {
		const (
			offset64 = 14695981039346656037
			prime64  = 1099511628211
		)
		var h uint64 = offset64
		for i := 0; i < len(id); i++ {
			h ^= uint64(id[i])
			h *= prime64
		}
		h ^= 0xff
		h *= prime64
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

	const batchSize = 50

	for {
		batches := make(map[string][]driverUpdate)

		for _, d := range drivers {
			d.lat += (rand.Float64()-0.5) * 0.0015
			d.lon += (rand.Float64()-0.5) * 0.0015
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

			if len(batches[targetURL]) >= batchSize {
				toSend := batches[targetURL]
				batches[targetURL] = nil
				go func(url string, payloadBatch []driverUpdate) {
					data, _ := json.Marshal(payloadBatch)
					req, _ := http.NewRequest("POST", url, bytes.NewBuffer(data))
					req.Header.Set("Content-Type", "application/json")
					resp, err := client.Do(req)
					if err == nil {
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
				}(targetURL, toSend)
			}
		}

		for url, batch := range batches {
			if len(batch) == 0 {
				continue
			}
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

		var countPending int
		if db != nil {
			_ = db.QueryRow("SELECT COUNT(1) FROM queue WHERE status IN ('pending','processing')").Scan(&countPending)
			promQueueDepth.Set(float64(countPending))
		}
		time.Sleep(interval)
	}
}