package db

import (
	"context"
	"testing"
	"time"
)

func TestHashForInode_andUpdateFileHash_sameScanHardlinkReuse(t *testing.T) {
	database := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, database, "/tmp")
	scan, _ := CreateScan(ctx, database, folderID)
	dev := int64(42)
	fileID1, _ := UpsertFile(ctx, database, folderID, "a", 100, 1, ptrInt64(999), &dev)
	InsertFileScan(ctx, database, fileID1, scan.ID)
	fileID2, _ := UpsertFile(ctx, database, folderID, "b", 100, 2, ptrInt64(999), &dev)
	InsertFileScan(ctx, database, fileID2, scan.ID)

	files, _ := GetFilesByScanID(ctx, database, scan.ID)
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}
	now := time.Now().UTC()
	const hashVal = "abc123"
	_ = UpdateFileHash(ctx, database, fileID1, hashVal, now)

	got, err := HashForInode(ctx, database, scan.ID, ptrInt64(999), &dev)
	if err != nil {
		t.Fatalf("HashForInode: %v", err)
	}
	if got != hashVal {
		t.Errorf("HashForInode = %q, want %q", got, hashVal)
	}

	_ = UpdateFileHash(ctx, database, fileID2, got, now)
	files2, _ := GetFilesByScanID(ctx, database, scan.ID)
	for _, f := range files2 {
		if f.Hash == nil || *f.Hash != hashVal {
			t.Errorf("file id=%d hash = %v, want %q", f.ID, f.Hash, hashVal)
		}
		if f.HashStatus != "done" {
			t.Errorf("file id=%d hash_status = %q, want done", f.ID, f.HashStatus)
		}
	}
}

func TestHashForInode_nilDeviceID(t *testing.T) {
	database := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, database, "/tmp")
	scan, _ := CreateScan(ctx, database, folderID)
	fileID1, _ := UpsertFile(ctx, database, folderID, "a", 10, 1, ptrInt64(111), nil)
	InsertFileScan(ctx, database, fileID1, scan.ID)
	fileID2, _ := UpsertFile(ctx, database, folderID, "b", 10, 2, ptrInt64(111), nil)
	InsertFileScan(ctx, database, fileID2, scan.ID)

	_ = UpdateFileHash(ctx, database, fileID1, "nildevhash", time.Now().UTC())
	got, err := HashForInode(ctx, database, scan.ID, ptrInt64(111), nil)
	if err != nil {
		t.Fatalf("HashForInode: %v", err)
	}
	if got != "nildevhash" {
		t.Errorf("HashForInode(inode 111, nil device) = %q, want nildevhash", got)
	}
}

func TestHashForInodeFromPreviousScan_unchangedFileReusesHash(t *testing.T) {
	database := TestPostgresDB(t)
	ctx := context.Background()

	f1, _ := AddFolder(ctx, database, "/tmp1")
	f2, _ := AddFolder(ctx, database, "/tmp2")
	scan1, _ := CreateScan(ctx, database, f1)
	scan2, _ := CreateScan(ctx, database, f2)
	dev := int64(1)
	fileID1, _ := UpsertFile(ctx, database, f1, "f", 100, 1, ptrInt64(123), &dev)
	InsertFileScan(ctx, database, fileID1, scan1.ID)
	_ = UpdateFileHash(ctx, database, fileID1, "abc", time.Now().UTC())

	fileID2, _ := UpsertFile(ctx, database, f2, "f", 100, 1, ptrInt64(123), &dev)
	InsertFileScan(ctx, database, fileID2, scan2.ID)

	got, err := HashForInodeFromPreviousScan(ctx, database, scan2.ID, ptrInt64(123), &dev, 100)
	if err != nil {
		t.Fatalf("HashForInodeFromPreviousScan: %v", err)
	}
	if got != "abc" {
		t.Errorf("HashForInodeFromPreviousScan = %q, want abc", got)
	}
}

func TestHashForInodeFromPreviousScan_differentSizeDoesNotReuse(t *testing.T) {
	database := TestPostgresDB(t)
	ctx := context.Background()

	f1, _ := AddFolder(ctx, database, "/tmp1")
	f2, _ := AddFolder(ctx, database, "/tmp2")
	scan1, _ := CreateScan(ctx, database, f1)
	scan2, _ := CreateScan(ctx, database, f2)
	dev := int64(1)
	fileID1, _ := UpsertFile(ctx, database, f1, "f", 100, 1, ptrInt64(123), &dev)
	InsertFileScan(ctx, database, fileID1, scan1.ID)
	_ = UpdateFileHash(ctx, database, fileID1, "abc", time.Now().UTC())

	fileID2, _ := UpsertFile(ctx, database, f2, "f", 200, 1, ptrInt64(123), &dev)
	InsertFileScan(ctx, database, fileID2, scan2.ID)

	got, err := HashForInodeFromPreviousScan(ctx, database, scan2.ID, ptrInt64(123), &dev, 200)
	if err != nil {
		t.Fatalf("HashForInodeFromPreviousScan: %v", err)
	}
	if got != "" {
		t.Errorf("HashForInodeFromPreviousScan(different size) = %q, want empty", got)
	}
}

func TestUpdateFileHashBatch(t *testing.T) {
	database := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, database, "/batch")
	scan, _ := CreateScan(ctx, database, folderID)
	fileID1, _ := UpsertFile(ctx, database, folderID, "x", 10, 1, ptrInt64(1), nil)
	InsertFileScan(ctx, database, fileID1, scan.ID)
	fileID2, _ := UpsertFile(ctx, database, folderID, "y", 20, 2, ptrInt64(2), nil)
	InsertFileScan(ctx, database, fileID2, scan.ID)

	now := time.Now().UTC()
	err := UpdateFileHashBatch(ctx, database, []HashUpdate{
		{FileID: fileID1, Hash: "h1", HashedAt: now},
		{FileID: fileID2, Hash: "h2", HashedAt: now},
	})
	if err != nil {
		t.Fatalf("UpdateFileHashBatch: %v", err)
	}

	files, _ := GetFilesByScanID(ctx, database, scan.ID)
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}
	for _, f := range files {
		if f.HashStatus != "done" {
			t.Errorf("file id=%d hash_status = %q", f.ID, f.HashStatus)
		}
		if f.ID == fileID1 && (f.Hash == nil || *f.Hash != "h1") {
			t.Errorf("file %d hash = %v, want h1", fileID1, f.Hash)
		}
		if f.ID == fileID2 && (f.Hash == nil || *f.Hash != "h2") {
			t.Errorf("file %d hash = %v, want h2", fileID2, f.Hash)
		}
	}
}

func TestHashForInode_differentScanDoesNotReuse(t *testing.T) {
	database := TestPostgresDB(t)
	ctx := context.Background()

	f1, _ := AddFolder(ctx, database, "/tmp1")
	f2, _ := AddFolder(ctx, database, "/tmp2")
	scan1, _ := CreateScan(ctx, database, f1)
	scan2, _ := CreateScan(ctx, database, f2)
	dev := int64(1)
	fileID1, _ := UpsertFile(ctx, database, f1, "f", 50, 1, ptrInt64(123), &dev)
	InsertFileScan(ctx, database, fileID1, scan1.ID)
	_ = UpdateFileHash(ctx, database, fileID1, "scan1hash", time.Now().UTC())

	fileID2, _ := UpsertFile(ctx, database, f2, "f", 50, 1, ptrInt64(123), &dev)
	InsertFileScan(ctx, database, fileID2, scan2.ID)

	got, err := HashForInode(ctx, database, scan2.ID, ptrInt64(123), &dev)
	if err != nil {
		t.Fatalf("HashForInode: %v", err)
	}
	if got != "" {
		t.Errorf("HashForInode(scan2, 123) = %q, want empty", got)
	}
}
