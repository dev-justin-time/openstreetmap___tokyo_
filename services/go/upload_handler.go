package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// lightweight instrument wrapper moved here for clarity
func instrumentHandler(name string, h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		timer := prometheus.NewTimer(promRequestDuration.WithLabelValues(name))
		defer timer.ObserveDuration()

		rr := &responseRecorder{ResponseWriter: w, status: 200}
		h(rr, r)
		promRequestsTotal.WithLabelValues(name, fmt.Sprintf("%d", rr.status)).Inc()
	}
}

func instrumentHandlerFunc(name string, h http.HandlerFunc) http.HandlerFunc {
	return instrumentHandler(name, h)
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// uploadHandler kept similar to original but uses enqueuePrimary/jobCh for async forwarding
func uploadHandler(w http.ResponseWriter, r *http.Request) {
	timer := prometheus.NewTimer(promRequestDuration.WithLabelValues("upload"))
	defer timer.ObserveDuration()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		promRequestsTotal.WithLabelValues("upload", "405").Inc()
		return
	}
	const maxSize = 25 << 20 // 25 MB
	err := r.ParseMultipartForm(maxSize)
	if err != nil {
		http.Error(w, "failed to parse multipart form: "+err.Error(), http.StatusBadRequest)
		promRequestsTotal.WithLabelValues("upload", "400").Inc()
		return
	}
	file, header, err := r.FormFile("gpx")
	if err != nil {
		http.Error(w, "missing gpx file", http.StatusBadRequest)
		promRequestsTotal.WithLabelValues("upload", "400").Inc()
		return
	}
	defer file.Close()

	var buf bytes.Buffer
	limited := io.LimitReader(file, maxSize+1)
	n, err := io.Copy(&buf, limited)
	if err != nil {
		http.Error(w, "failed to read file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if n > maxSize {
		http.Error(w, "file too large (max 25MB)", http.StatusRequestEntityTooLarge)
		return
	}
	rawBytes := buf.Bytes()

	contentType := http.DetectContentType(rawBytes)
	allowed := false
	if containsXML(contentType) || contentType == "application/octet-stream" {
		allowed = true
	}
	if !allowed && hasGPXExt(header.Filename) {
		allowed = true
	}
	if !allowed {
		http.Error(w, "unsupported file type: "+contentType, http.StatusUnsupportedMediaType)
		return
	}
	if !bytes.Contains(bytes.ToLower(rawBytes), []byte("<gpx")) {
		http.Error(w, "invalid GPX: missing <gpx> element", http.StatusBadRequest)
		return
	}

	var count int
	if db != nil {
		_ = db.QueryRow("SELECT COUNT(1) FROM queue WHERE status IN ('pending','processing')").Scan(&count)
		promQueueDepth.Set(float64(count))
	}

	if !breakerAllow() {
		if _, err := enqueuePrimary([]byte(`{"status":"deferred","reason":"circuit_open_on_receive"}`), rawBytes); err != nil {
			http.Error(w, "failed to enqueue fallback: "+err.Error(), http.StatusInternalServerError)
			promRequestsTotal.WithLabelValues("upload", "500").Inc()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"queued","reason":"circuit_open"}`))
		promRequestsTotal.WithLabelValues("upload", "503").Inc()
		return
	}

	job := &forwardJob{
		rawBytes: rawBytes,
		filename: header.Filename,
		respChan: make(chan forwardResult, 1),
		attempts: 0,
	}

	select {
	case jobCh <- job:
		select {
		case res := <-job.respChan:
			if res.err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				w.Write([]byte(`{"status":"deferred","note":"worker_error"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(res.statusCode)
			w.Write(res.body)
			return
		case <-time.After(800 * time.Millisecond):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"status":"queued","note":"processing_async"}`))
			return
		}
	default:
		if _, err := enqueuePrimary([]byte(`{"status":"deferred","reason":"queue_full_on_receive"}`), rawBytes); err != nil {
			http.Error(w, "failed to enqueue fallback: "+err.Error(), http.StatusInternalServerError)
			promRequestsTotal.WithLabelValues("upload", "500").Inc()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"queued","reason":"queue_full"}`))
		promRequestsTotal.WithLabelValues("upload", "503").Inc()
		return
	}
}

func containsXML(t string) bool {
	return strings.Contains(t, "xml") || strings.Contains(t, "text")
}

func hasGPXExt(n string) bool {
	n = strings.ToLower(n)
	return strings.HasSuffix(n, ".gpx")
}