package main

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// Prometheus metrics
var (
	promRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "go_gateway_http_requests_total",
			Help: "Total HTTP requests processed by the Go gateway, labeled by handler and status.",
		},
		[]string{"handler", "status"},
	)
	promRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "go_gateway_request_duration_seconds",
			Help:    "Request processing duration in seconds by handler.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"handler"},
	)
	promQueueDepth = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "go_gateway_queue_depth",
			Help: "Number of entries in the SQLite metadata queue (approx).",
		},
	)
	promWorkerForwardFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "go_gateway_worker_forward_failures_total",
			Help: "Total forward failures from workers to Rust.",
		},
	)
	promWorkerForwards = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "go_gateway_worker_forwards_total",
			Help: "Total forward attempts from workers to Rust.",
		},
	)
)

func registerMetrics() {
	prometheus.MustRegister(promRequestsTotal, promRequestDuration, promQueueDepth, promWorkerForwardFailures, promWorkerForwards)
	fmt.Println("Prometheus metrics registered")
}