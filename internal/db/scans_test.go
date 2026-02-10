package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func TestCreateScan_returnsScanWithIDAndCreatedAt(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	scan, err := CreateScan(ctx, db, "/tmp/photos")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}
	if scan.ID <= 0 {
		t.Errorf("CreateScan ID = %d, want > 0", scan.ID)
	}
	if scan.CreatedAt.IsZero() {
		t.Error("CreateScan CreatedAt is zero")
	}
	if scan.RootPath != "/tmp/photos" {
		t.Errorf("RootPath = %q, want %q", scan.RootPath, "/tmp/photos")
	}

	got, err := GetScan(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("GetScan: %v", err)
	}
	if got.ID != scan.ID || got.RootPath != scan.RootPath || !got.CreatedAt.Equal(scan.CreatedAt) {
		t.Errorf("GetScan = %+v, want %+v", got, scan)
	}
}

func TestGetScan_notFound(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	_, err := GetScan(ctx, db, 99999)
	if err == nil {
		t.Fatal("GetScan(99999): want error, got nil")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetScan err = %v, want sql.ErrNoRows", err)
	}
}

func TestListScans_emptyReturnsEmptySlice(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	scans, err := ListScans(ctx, db)
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if len(scans) != 0 {
		t.Errorf("ListScans = %v, want empty slice", scans)
	}
}

func TestListScans_newestFirst(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	s1, err := CreateScan(ctx, db, "/first")
	if err != nil {
		t.Fatalf("CreateScan first: %v", err)
	}
	s2, err := CreateScan(ctx, db, "/second")
	if err != nil {
		t.Fatalf("CreateScan second: %v", err)
	}

	scans, err := ListScans(ctx, db)
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if len(scans) != 2 {
		t.Fatalf("ListScans len = %d, want 2", len(scans))
	}
	if scans[0].ID != s2.ID || scans[1].ID != s1.ID {
		t.Errorf("ListScans order: got [%d, %d], want newest first [%d, %d]",
			scans[0].ID, scans[1].ID, s2.ID, s1.ID)
	}
}

func TestGetLatestIncompleteScanForRoot(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// No scans: returns 0
	id, err := GetLatestIncompleteScanForRoot(ctx, db, "/any")
	if err != nil {
		t.Fatalf("GetLatestIncompleteScanForRoot: %v", err)
	}
	if id != 0 {
		t.Errorf("empty db: got %d, want 0", id)
	}

	// One incomplete scan for /foo
	s1, _ := CreateScan(ctx, db, "/foo")
	id, err = GetLatestIncompleteScanForRoot(ctx, db, "/foo")
	if err != nil {
		t.Fatalf("GetLatestIncompleteScanForRoot: %v", err)
	}
	if id != s1.ID {
		t.Errorf("one incomplete: got %d, want %d", id, s1.ID)
	}

	// Complete s1, then create s2 for same root: returns s2 (latest incomplete)
	_ = UpdateScanCompletedAt(ctx, db, s1.ID, 0, 0)
	s2, _ := CreateScan(ctx, db, "/foo")
	id, _ = GetLatestIncompleteScanForRoot(ctx, db, "/foo")
	if id != s2.ID {
		t.Errorf("after complete first: got %d, want %d (latest incomplete)", id, s2.ID)
	}

	// Different root has no incomplete
	id, _ = GetLatestIncompleteScanForRoot(ctx, db, "/other")
	if id != 0 {
		t.Errorf("other root: got %d, want 0", id)
	}
}
