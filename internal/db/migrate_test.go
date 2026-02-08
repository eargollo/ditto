package db

import (
	"context"
	"database/sql"
	"testing"
)

func TestMigrate_createsScansAndFilesTables(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	err = Migrate(db)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	rows, err := db.QueryContext(context.Background(),
		"SELECT name FROM sqlite_master WHERE type='table' AND name IN ('scans', 'files') ORDER BY name")
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	if len(tables) != 2 {
		t.Fatalf("got %d tables %v, want 2 (files, scans)", len(tables), tables)
	}
	if tables[0] != "files" || tables[1] != "scans" {
		t.Errorf("tables = %v, want [files scans]", tables)
	}
}

func TestMigrate_insertAndSelectRoundTrip(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	err = Migrate(db)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	ctx := context.Background()

	res, err := db.ExecContext(ctx,
		"INSERT INTO scans (created_at, completed_at, root_path) VALUES (?, ?, ?)",
		"2025-02-07T10:00:00Z", nil, "/tmp")
	if err != nil {
		t.Fatalf("insert scan: %v", err)
	}
	scanID, _ := res.LastInsertId()
	if scanID <= 0 {
		t.Fatal("scan ID not returned")
	}

	_, err = db.ExecContext(ctx,
		"INSERT INTO files (scan_id, path, size, mtime, inode, device_id, hash, hash_status, hashed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		scanID, "/tmp/foo", 100, 1707292800, 12345, 1, nil, "pending", nil)
	if err != nil {
		t.Fatalf("insert file: %v", err)
	}

	var path string
	var size int64
	var mtime int64
	var inode int64
	var deviceID sql.NullInt64
	var hash sql.NullString
	var hashStatus string
	var hashedAt sql.NullString
	err = db.QueryRowContext(ctx,
		"SELECT path, size, mtime, inode, device_id, hash, hash_status, hashed_at FROM files WHERE scan_id = ?", scanID).
		Scan(&path, &size, &mtime, &inode, &deviceID, &hash, &hashStatus, &hashedAt)
	if err != nil {
		t.Fatalf("select file: %v", err)
	}
	if path != "/tmp/foo" || size != 100 || mtime != 1707292800 || inode != 12345 || hashStatus != "pending" {
		t.Errorf("file: path=%q size=%d mtime=%d inode=%d hash_status=%q", path, size, mtime, inode, hashStatus)
	}
	if !deviceID.Valid || deviceID.Int64 != 1 {
		t.Errorf("device_id: got %+v", deviceID)
	}
	if hash.Valid || hashedAt.Valid {
		t.Errorf("hash and hashed_at should be null: hash=%+v hashed_at=%+v", hash, hashedAt)
	}
}
