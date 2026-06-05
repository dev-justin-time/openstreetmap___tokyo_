package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var db *sql.DB

// initDB initializes the SQLite queue and store directory.
func initDB() error {
	if err := os.MkdirAll("queue_store", 0o755); err != nil {
		return err
	}
	dbPath := filepath.Join("queue_store", "queue.db")
	var err error
	db, err = sql.Open("sqlite3", dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return err
	}
	schema := `
	CREATE TABLE IF NOT EXISTS queue (
	  id INTEGER PRIMARY KEY AUTOINCREMENT,
	  created_at INTEGER NOT NULL,
	  payload_ref TEXT NOT NULL,
	  primary_json TEXT,
	  status TEXT NOT NULL DEFAULT 'pending',
	  attempts INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_queue_status_created ON queue(status, created_at);

	-- driver_index: a small table mapping driver logical id to a numeric rowid (for RTree)
	CREATE TABLE IF NOT EXISTS driver_index (
	  rowid INTEGER PRIMARY KEY AUTOINCREMENT,
	  driver_key TEXT NOT NULL UNIQUE,
	  lat REAL NOT NULL,
	  lon REAL NOT NULL,
	  ts INTEGER NOT NULL
	);

	-- RTree virtual table using the driver_index.rowid for spatial searching.
	-- columns: rowid, min_lat, max_lat, min_lon, max_lon
	CREATE VIRTUAL TABLE IF NOT EXISTS driver_rtree USING rtree(
	  rowid,
	  min_lat, max_lat,
	  min_lon, max_lon
	);

	-- trigger to keep rtree in sync when inserting or updating driver_index
	CREATE TRIGGER IF NOT EXISTS driver_index_after_insert AFTER INSERT ON driver_index
	BEGIN
	  INSERT OR REPLACE INTO driver_rtree(rowid, min_lat, max_lat, min_lon, max_lon)
	  VALUES (new.rowid, new.lat, new.lat, new.lon, new.lon);
	END;
	CREATE TRIGGER IF NOT EXISTS driver_index_after_update AFTER UPDATE ON driver_index
	BEGIN
	  INSERT OR REPLACE INTO driver_rtree(rowid, min_lat, max_lat, min_lon, max_lon)
	  VALUES (new.rowid, new.lat, new.lat, new.lon, new.lon);
	END;
	CREATE TRIGGER IF NOT EXISTS driver_index_after_delete AFTER DELETE ON driver_index
	BEGIN
	  DELETE FROM driver_rtree WHERE rowid = old.rowid;
	END;
	`
	_, err = db.Exec(schema)
	return err
}

func enqueuePrimary(primaryJson []byte, rawBytes []byte) (int64, error) {
	// persist raw backup file to queue_store/payload_<id_timestamp>.gpx
	ts := time.Now().UnixNano()
	fn := fmt.Sprintf("payload_%d.gpx", ts)
	path := filepath.Join("queue_store", fn)
	if err := os.WriteFile(path, rawBytes, 0o644); err != nil {
		return 0, err
	}

	res, err := db.Exec("INSERT INTO queue (created_at, payload_ref, primary_json, status, attempts) VALUES (?, ?, ?, 'pending', 0)",
		time.Now().Unix(), path, string(primaryJson))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func fetchPendingBatch(limit int) ([]map[string]interface{}, error) {
	rows, err := db.Query("SELECT id, created_at, payload_ref, primary_json, status, attempts FROM queue WHERE status = 'pending' ORDER BY created_at LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var created int64
		var payloadRef string
		var primaryJson sql.NullString
		var status string
		var attempts int
		if err := rows.Scan(&id, &created, &payloadRef, &primaryJson, &status, &attempts); err != nil {
			return nil, err
		}
		item := map[string]interface{}{
			"id":         id,
			"created_at": created,
			"payload_ref": payloadRef,
			"primary_json": nil,
			"status":     status,
			"attempts":   attempts,
		}
		if primaryJson.Valid {
			item["primary_json"] = primaryJson.String
		}
		result = append(result, item)
	}
	return result, nil
}

func markProcessing(id int64) error {
	_, err := db.Exec("UPDATE queue SET status='processing', attempts=attempts+1 WHERE id = ? AND status = 'pending'", id)
	return err
}

func markDone(id int64) error {
	_, err := db.Exec("UPDATE queue SET status='done' WHERE id = ?", id)
	return err
}