package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"
	"strings"
	"sync/atomic"
)

// worker configuration kept local to this file
const (
	workerPoolSize      = 4
	maxForwardAttempts  = 4
	backoffBase         = 500
	breakerFailThreshold = 6
	breakerOpenMillis    = 5000
)

type forwardJob struct {
	rawBytes []byte
	filename string
	respChan chan forwardResult
	attempts int
}

type forwardResult struct {
	statusCode int
	body       []byte
	err        error
}

var jobCh = make(chan *forwardJob, 128)

// circuit-breaker state
var breakerFailures int32
var breakerOpenUntil int64 // unix millis

func breakerAllow() bool {
	now := time.Now().UnixMilli()
	openUntil := atomic.LoadInt64(&breakerOpenUntil)
	return now >= openUntil
}

func breakerRecordFailure() {
	f := atomic.AddInt32(&breakerFailures, 1)
	if f >= breakerFailThreshold {
		atomic.StoreInt64(&breakerOpenUntil, time.Now().Add(time.Millisecond*breakerOpenMillis).UnixMilli())
		atomic.StoreInt32(&breakerFailures, 0)
	}
}

func breakerRecordSuccess() {
	atomic.StoreInt32(&breakerFailures, 0)
	atomic.StoreInt64(&breakerOpenUntil, 0)
}

func startWorkerPool() {
	for i := 0; i < workerPoolSize; i++ {
		go workerLoop(i)
	}
}

func workerLoop(index int) {
	client := &http.Client{Timeout: 15 * time.Second}

	for job := range jobCh {
		if job == nil {
			continue
		}
		promWorkerForwards.Inc()
		if !breakerAllow() {
			promWorkerForwardFailures.Inc()
			if _, err := enqueuePrimary([]byte(`{"status":"deferred","reason":"circuit_open"}`), job.rawBytes); err != nil {
				log.Println("worker enqueue fallback failed:", err)
			}
			if job.respChan != nil {
				job.respChan <- forwardResult{statusCode: http.StatusServiceUnavailable, body: []byte(`{"error":"circuit_open"}`), err: nil}
			}
			continue
		}

		var b bytes.Buffer
		writer := multipart.NewWriter(&b)
		part, err := writer.CreateFormFile("gpx", job.filename)
		if err != nil {
			log.Println("worker failed to create multipart part:", err)
			job.attempts++
			promWorkerForwardFailures.Inc()
			handleJobRetryOrEnqueue(job, nil)
			continue
		}
		if _, err := io.Copy(part, bytes.NewReader(job.rawBytes)); err != nil {
			log.Println("worker failed to write multipart body:", err)
			job.attempts++
			promWorkerForwardFailures.Inc()
			handleJobRetryOrEnqueue(job, nil)
			writer.Close()
			continue
		}
		_ = writer.WriteField("filename", job.filename)
		_ = writer.Close()

		req, err := http.NewRequest("POST", "http://127.0.0.1:3030/process-gpx", &b)
		if err != nil {
			log.Println("worker failed to build request:", err)
			job.attempts++
			promWorkerForwardFailures.Inc()
			handleJobRetryOrEnqueue(job, nil)
			continue
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())

		if strings.Contains(job.filename, "traceparent=") {
			parts := strings.Split(job.filename, "traceparent=")
			if len(parts) > 1 {
				trace := parts[1]
				req.Header.Set("traceparent", trace)
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			log.Println("worker forward error:", err)
			breakerRecordFailure()
			job.attempts++
			promWorkerForwardFailures.Inc()
			handleJobRetryOrEnqueue(job, nil)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Printf("worker non-2xx (%d): %s\n", resp.StatusCode, string(body))
			breakerRecordFailure()
			job.attempts++
			promWorkerForwardFailures.Inc()
			handleJobRetryOrEnqueue(job, &forwardResult{statusCode: resp.StatusCode, body: body, err: fmt.Errorf("non-2xx")})
			continue
		}

		// success
		breakerRecordSuccess()
		if _, err := enqueuePrimary(body, job.rawBytes); err != nil {
			log.Println("worker failed to enqueue primary after success:", err)
			if job.respChan != nil {
				job.respChan <- forwardResult{statusCode: resp.StatusCode, body: body, err: err}
			}
			continue
		}
		if job.respChan != nil {
			job.respChan <- forwardResult{statusCode: resp.StatusCode, body: body, err: nil}
		}
	}
}

func handleJobRetryOrEnqueue(job *forwardJob, lastResult *forwardResult) {
	if job.attempts < maxForwardAttempts {
		delay := time.Duration(backoffBase*(1<<uint(job.attempts-1))) * time.Millisecond
		go func(j *forwardJob, d time.Duration) {
			time.Sleep(d)
			select {
			case jobCh <- j:
			default:
				promWorkerForwardFailures.Inc()
				if _, err := enqueuePrimary([]byte(`{"status":"deferred","reason":"queue_full_on_retry"}`), j.rawBytes); err != nil {
					log.Println("enqueue fallback after queue full failed:", err)
				}
			}
		}(job, delay)
	} else {
		promWorkerForwardFailures.Inc()
		_, err := enqueuePrimary([]byte(`{"status":"deferred","reason":"max_attempts_exceeded"}`), job.rawBytes)
		if err != nil {
			log.Println("enqueuePrimary fallback failed after max attempts:", err)
		}
		if job.respChan != nil {
			job.respChan <- forwardResult{statusCode: http.StatusAccepted, body: []byte(`{"status":"deferred"}`), err: fmt.Errorf("max attempts exceeded")}
		}
	}
}