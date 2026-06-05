package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type LocalOrder struct {
	ID              string
	PickupLat       float64
	PickupLon       float64
	DropoffLat      float64
	DropoffLon      float64
	Status          string
	AssignedDriverID string
	CreatedAt       string
	UpdatedAt       string
	Synced          bool
}

type LocalStore struct {
	db *sql.DB
}

func NewLocalStore(dir string) (*LocalStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, "driver.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=3000")
	if err != nil {
		return nil, err
	}
	schema := `
	CREATE TABLE IF NOT EXISTS local_orders (
		id TEXT PRIMARY KEY,
		pickup_lat REAL NOT NULL,
		pickup_lon REAL NOT NULL,
		dropoff_lat REAL NOT NULL,
		dropoff_lon REAL NOT NULL,
		status TEXT NOT NULL DEFAULT 'assigned',
		assigned_driver_id TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		synced INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS driver_state (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &LocalStore{db: db}, nil
}

func (s *LocalStore) SaveOrder(o LocalOrder) error {
	_, err := s.db.Exec(`
		INSERT INTO local_orders (id, pickup_lat, pickup_lon, dropoff_lat, dropoff_lon, status, assigned_driver_id, created_at, updated_at, synced)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			updated_at = excluded.updated_at,
			synced = excluded.synced`,
		o.ID, o.PickupLat, o.PickupLon, o.DropoffLat, o.DropoffLon, o.Status, o.AssignedDriverID, o.CreatedAt, o.UpdatedAt, boolInt(o.Synced))
	return err
}

func (s *LocalStore) GetOrder(id string) (*LocalOrder, error) {
	o := &LocalOrder{}
	var synced int
	err := s.db.QueryRow(`
		SELECT id, pickup_lat, pickup_lon, dropoff_lat, dropoff_lon, status, COALESCE(assigned_driver_id,''), created_at, updated_at, synced
		FROM local_orders WHERE id = ?`, id).Scan(
		&o.ID, &o.PickupLat, &o.PickupLon, &o.DropoffLat, &o.DropoffLon, &o.Status, &o.AssignedDriverID, &o.CreatedAt, &o.UpdatedAt, &synced)
	if err != nil {
		return nil, err
	}
	o.Synced = synced != 0
	return o, nil
}

func (s *LocalStore) ListPendingOrders() ([]LocalOrder, error) {
	rows, err := s.db.Query(`
		SELECT id, pickup_lat, pickup_lon, dropoff_lat, dropoff_lon, status, COALESCE(assigned_driver_id,''), created_at, updated_at, synced
		FROM local_orders WHERE status NOT IN ('delivered','cancelled') ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []LocalOrder
	for rows.Next() {
		var o LocalOrder
		var synced int
		if err := rows.Scan(&o.ID, &o.PickupLat, &o.PickupLon, &o.DropoffLat, &o.DropoffLon, &o.Status, &o.AssignedDriverID, &o.CreatedAt, &o.UpdatedAt, &synced); err != nil {
			continue
		}
		o.Synced = synced != 0
		orders = append(orders, o)
	}
	return orders, nil
}

func (s *LocalStore) UpdateOrderStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE local_orders SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *LocalStore) GetUnsyncedOrders() ([]LocalOrder, error) {
	rows, err := s.db.Query(`
		SELECT id, pickup_lat, pickup_lon, dropoff_lat, dropoff_lon, status, COALESCE(assigned_driver_id,''), created_at, updated_at, synced
		FROM local_orders WHERE synced = 0 ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []LocalOrder
	for rows.Next() {
		var o LocalOrder
		var synced int
		if err := rows.Scan(&o.ID, &o.PickupLat, &o.PickupLon, &o.DropoffLat, &o.DropoffLon, &o.Status, &o.AssignedDriverID, &o.CreatedAt, &o.UpdatedAt, &synced); err != nil {
			continue
		}
		o.Synced = synced != 0
		orders = append(orders, o)
	}
	return orders, nil
}

func (s *LocalStore) MarkSynced(id string) error {
	_, err := s.db.Exec(`UPDATE local_orders SET synced = 1 WHERE id = ?`, id)
	return err
}

func (s *LocalStore) SetState(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO driver_state (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *LocalStore) GetState(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM driver_state WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (s *LocalStore) Close() error {
	return s.db.Close()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm%ds", m, s)
}
