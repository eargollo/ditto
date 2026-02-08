package db

import (
	"context"
	"testing"
)

func TestInsertFile_andGetFilesByScanID(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	scan, err := CreateScan(ctx, db, "/tmp")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}
	deviceID := int64(42)
	err = InsertFile(ctx, db, scan.ID, "/tmp/foo", 100, 1707292800, 12345, &deviceID)
	if err != nil {
		t.Fatalf("InsertFile: %v", err)
	}

	files, err := GetFilesByScanID(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("GetFilesByScanID: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("GetFilesByScanID len = %d, want 1", len(files))
	}
	f := files[0]
	if f.Path != "/tmp/foo" || f.Size != 100 || f.MTime != 1707292800 || f.Inode != 12345 {
		t.Errorf("file: path=%q size=%d mtime=%d inode=%d", f.Path, f.Size, f.MTime, f.Inode)
	}
	if f.HashStatus != "pending" {
		t.Errorf("hash_status = %q, want pending", f.HashStatus)
	}
	if f.Hash != nil || f.HashedAt != nil {
		t.Errorf("hash and hashed_at should be nil: hash=%v hashed_at=%v", f.Hash, f.HashedAt)
	}
	if f.DeviceID == nil || *f.DeviceID != 42 {
		t.Errorf("device_id = %v, want 42", f.DeviceID)
	}
}

func TestGetFilesByScanID_emptyReturnsEmptySlice(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	scan, err := CreateScan(ctx, db, "/tmp")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}

	files, err := GetFilesByScanID(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("GetFilesByScanID: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("GetFilesByScanID = %v, want empty slice", files)
	}
}

func TestGetFilesByScanID_multiple(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	scan, err := CreateScan(ctx, db, "/tmp")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}
	if err := InsertFile(ctx, db, scan.ID, "/tmp/a", 10, 100, 1, nil); err != nil {
		t.Fatalf("InsertFile a: %v", err)
	}
	if err := InsertFile(ctx, db, scan.ID, "/tmp/b", 20, 200, 2, nil); err != nil {
		t.Fatalf("InsertFile b: %v", err)
	}

	files, err := GetFilesByScanID(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("GetFilesByScanID: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("GetFilesByScanID len = %d, want 2", len(files))
	}
	paths := map[string]bool{}
	for _, f := range files {
		paths[f.Path] = true
	}
	if !paths["/tmp/a"] || !paths["/tmp/b"] {
		t.Errorf("files = %v", files)
	}
}
