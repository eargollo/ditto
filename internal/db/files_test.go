package db

import (
	"context"
	"testing"
)

func TestUpsertFile_InsertFileScan_and_GetFilesByScanID(t *testing.T) {
	db := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	deviceID := int64(42)
	fileID, err := UpsertFile(ctx, db, folderID, "foo", 100, 1707292800, 12345, &deviceID)
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	if err := InsertFileScan(ctx, db, fileID, scan.ID); err != nil {
		t.Fatalf("InsertFileScan: %v", err)
	}

	files, err := GetFilesByScanID(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("GetFilesByScanID: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("GetFilesByScanID len = %d, want 1", len(files))
	}
	f := files[0]
	if f.Size != 100 || f.MTime != 1707292800 || f.Inode != 12345 {
		t.Errorf("file: size=%d mtime=%d inode=%d", f.Size, f.MTime, f.Inode)
	}
	if f.HashStatus != "pending" {
		t.Errorf("hash_status = %q, want pending", f.HashStatus)
	}
	if f.DeviceID == nil || *f.DeviceID != 42 {
		t.Errorf("device_id = %v, want 42", f.DeviceID)
	}
}

func TestGetFilesByScanID_emptyReturnsEmptySlice(t *testing.T) {
	db := TestPostgresDB(t)
	ctx := context.Background()

	folderID, err := AddFolder(ctx, db, "/tmp")
	if err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	scan, err := CreateScan(ctx, db, folderID)
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
	db := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	fileID1, _ := UpsertFile(ctx, db, folderID, "a", 10, 100, 1, nil)
	InsertFileScan(ctx, db, fileID1, scan.ID)
	fileID2, _ := UpsertFile(ctx, db, folderID, "b", 20, 200, 2, nil)
	InsertFileScan(ctx, db, fileID2, scan.ID)

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
	// Path is folder path + "/" + relative
	if !paths["/tmp/a"] {
		t.Errorf("want path /tmp/a in %v", paths)
	}
	if !paths["/tmp/b"] {
		t.Errorf("want path /tmp/b in %v", paths)
	}
}

func TestUpsertFilesBatch_InsertFileScanBatch(t *testing.T) {
	database := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, database, "/tmp")
	scan, _ := CreateScan(ctx, database, folderID)
	dev := int64(1)
	rows := []FileRow{
		{Path: "a", Size: 10, MTime: 100, Inode: 1, DeviceID: &dev},
		{Path: "b", Size: 20, MTime: 200, Inode: 2, DeviceID: nil},
	}
	ids, err := UpsertFilesBatch(ctx, database, folderID, rows)
	if err != nil {
		t.Fatalf("UpsertFilesBatch: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("UpsertFilesBatch returned %d ids, want 2", len(ids))
	}
	if err := InsertFileScanBatch(ctx, database, ids, scan.ID); err != nil {
		t.Fatalf("InsertFileScanBatch: %v", err)
	}
	files, err := GetFilesByScanID(ctx, database, scan.ID)
	if err != nil {
		t.Fatalf("GetFilesByScanID: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("GetFilesByScanID len = %d, want 2", len(files))
	}
	byPath := make(map[string]File)
	for _, f := range files {
		byPath[f.Path] = f
	}
	if byPath["/tmp/a"].Size != 10 || byPath["/tmp/b"].Size != 20 {
		t.Errorf("batch insert sizes: a=%d b=%d", byPath["/tmp/a"].Size, byPath["/tmp/b"].Size)
	}
}
