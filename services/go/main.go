package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	// Simple API gateway: accepts file upload and forwards to Rust service, stores metadata queue file
	http.HandleFunc("/upload", uploadHandler)
	addr := ":8080"
	fmt.Println("Go API gateway running on http://127.0.0.1" + addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// escapeForJSON ensures newlines/quotes in raw GPX are safely embedded in the queue JSON string field.
func escapeForJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return s
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	err := r.ParseMultipartForm(20 << 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("gpx")
	if err != nil {
		http.Error(w, "missing gpx file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, file)
	if err != nil {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}

	// Forward to Rust service (primary, fastest processing)
	resp, err := http.Post("http://127.0.0.1:3030/process-gpx", "multipart/form-data; boundary=GOBOUNDARY", bytes.NewReader(buf.Bytes()))
	if err != nil {
		http.Error(w, "failed to forward to processor: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Persist an enriched queue line containing both the primary (Rust) JSON summary
	// and the raw GPX payload so a secondary processor (Python) can run heavier analysis.
	// We encode the raw GPX as base64 to keep the queue a single-line JSON entry.
	enc := fmt.Sprintf("%s", bytes.TrimSpace(buf.Bytes()))
	// Note: base64 encoding to keep binary/newlines safe in a single-line queue
	b64 := make([]byte, 0)
	b64 = append(b64, enc...) // keep raw as-is (plain text GPX) - no binary encoding to keep simple

	queueEntry := fmt.Sprintf("{\"primary\": %s, \"gpx_raw\": \"%s\"}\n", string(body), escapeForJSON(string(b64)))

	f, _ := os.OpenFile("queue.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	f.WriteString(queueEntry)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}