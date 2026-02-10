package db

import (
	"context"
	"testing"
	"time"
)

func TestHashForInode_andUpdateFileHash_sameScanHardlinkReuse(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()

	scan, err := CreateScan(ctx, database, "/tmp")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}
	dev := int64(42)
	// Two files same inode+device (hardlinks)
	if err := InsertFile(ctx, database, scan.ID, "/tmp/a", 100, 1, 999, &dev); err != nil {
		t.Fatalf("InsertFile a: %v", err)
	}
	if err := InsertFile(ctx, database, scan.ID, "/tmp/b", 100, 2, 999, &dev); err != nil {
		t.Fatalf("InsertFile b: %v", err)
	}

	files, err := GetFilesByScanID(ctx, database, scan.ID)
	if err != nil {
		t.Fatalf("GetFilesByScanID: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}
	firstID, secondID := files[0].ID, files[1].ID
	now := time.Now().UTC()

	// Simulate: we hashed the first file
	const hashVal = "abc123"
	if err := UpdateFileHash(ctx, database, firstID, hashVal, now); err != nil {
		t.Fatalf("UpdateFileHash first: %v", err)
	}

	// Same-scan inode lookup should return that hash
	got, err := HashForInode(ctx, database, scan.ID, 999, &dev)
	if err != nil {
		t.Fatalf("HashForInode: %v", err)
	}
	if got != hashVal {
		t.Errorf("HashForInode = %q, want %q", got, hashVal)
	}

	// Copy hash to second row (simulating hardlink reuse)
	if err := UpdateFileHash(ctx, database, secondID, got, now); err != nil {
		t.Fatalf("UpdateFileHash second: %v", err)
	}

	// Both rows should have same hash and done
	files2, _ := GetFilesByScanID(ctx, database, scan.ID)
	for _, f := range files2 {
		if f.Hash == nil || *f.Hash != hashVal {
			t.Errorf("file id=%d hash = %v, want %q", f.ID, f.Hash, hashVal)
		}
		if f.HashStatus != "done" {
			t.Errorf("file id=%d hash_status = %q, want done", f.ID, f.HashStatus)
		}
		if f.HashedAt == nil {
			t.Errorf("file id=%d hashed_at = nil", f.ID)
		}
	}
}

func TestHashForInode_nilDeviceID(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()

	scan, err := CreateScan(ctx, database, "/tmp")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}
	// Two files same inode, device_id NULL (e.g. OS that does not provide device)
	if err := InsertFile(ctx, database, scan.ID, "/tmp/a", 10, 1, 111, nil); err != nil {
		t.Fatalf("InsertFile a: %v", err)
	}
	if err := InsertFile(ctx, database, scan.ID, "/tmp/b", 10, 2, 111, nil); err != nil {
		t.Fatalf("InsertFile b: %v", err)
	}
	files, _ := GetFilesByScanID(ctx, database, scan.ID)
	if err := UpdateFileHash(ctx, database, files[0].ID, "nildevhash", time.Now().UTC()); err != nil {
		t.Fatalf("UpdateFileHash: %v", err)
	}

	got, err := HashForInode(ctx, database, scan.ID, 111, nil)
	if err != nil {
		t.Fatalf("HashForInode: %v", err)
	}
	if got != "nildevhash" {
		t.Errorf("HashForInode(inode 111, nil device) = %q, want nildevhash", got)
	}
}

func TestHashForInodeFromPreviousScan_unchangedFileReusesHash(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()

	scan1, _ := CreateScan(ctx, database, "/tmp1")
	scan2, _ := CreateScan(ctx, database, "/tmp2")
	dev := int64(1)
	if err := InsertFile(ctx, database, scan1.ID, "/tmp1/f", 100, 1, 123, &dev); err != nil {
		t.Fatalf("InsertFile scan1: %v", err)
	}
	files1, _ := GetFilesByScanID(ctx, database, scan1.ID)
	if err := UpdateFileHash(ctx, database, files1[0].ID, "abc", time.Now().UTC()); err != nil {
		t.Fatalf("UpdateFileHash: %v", err)
	}
	// Scan 2: same file (inode 123, size 100) — unchanged
	if err := InsertFile(ctx, database, scan2.ID, "/tmp2/f", 100, 1, 123, &dev); err != nil {
		t.Fatalf("InsertFile scan2: %v", err)
	}

	got, err := HashForInodeFromPreviousScan(ctx, database, scan2.ID, 123, &dev, 100)
	if err != nil {
		t.Fatalf("HashForInodeFromPreviousScan: %v", err)
	}
	if got != "abc" {
		t.Errorf("HashForInodeFromPreviousScan = %q, want abc", got)
	}
}

func TestHashForInodeFromPreviousScan_differentSizeDoesNotReuse(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()

	scan1, _ := CreateScan(ctx, database, "/tmp1")
	scan2, _ := CreateScan(ctx, database, "/tmp2")
	dev := int64(1)
	if err := InsertFile(ctx, database, scan1.ID, "/tmp1/f", 100, 1, 123, &dev); err != nil {
		t.Fatalf("InsertFile scan1: %v", err)
	}
	files1, _ := GetFilesByScanID(ctx, database, scan1.ID)
	if err := UpdateFileHash(ctx, database, files1[0].ID, "abc", time.Now().UTC()); err != nil {
		t.Fatalf("UpdateFileHash: %v", err)
	}
	// Scan 2: same inode but different size (file changed)
	if err := InsertFile(ctx, database, scan2.ID, "/tmp2/f", 200, 1, 123, &dev); err != nil {
		t.Fatalf("InsertFile scan2: %v", err)
	}

	got, err := HashForInodeFromPreviousScan(ctx, database, scan2.ID, 123, &dev, 200)
	if err != nil {
		t.Fatalf("HashForInodeFromPreviousScan: %v", err)
	}
	if got != "" {
		t.Errorf("HashForInodeFromPreviousScan(different size) = %q, want empty", got)
	}
}

func TestHashForInode_differentScanDoesNotReuse(t *testing.T) {
	database := testDB(t)
	ctx := context.Background()

	scan1, err := CreateScan(ctx, database, "/tmp1")
	if err != nil {
		t.Fatalf("CreateScan 1: %v", err)
	}
	scan2, err := CreateScan(ctx, database, "/tmp2")
	if err != nil {
		t.Fatalf("CreateScan 2: %v", err)
	}
	dev := int64(1)
	// Scan 1: file with inode 123, hashed
	if err := InsertFile(ctx, database, scan1.ID, "/tmp1/f", 50, 1, 123, &dev); err != nil {
		t.Fatalf("InsertFile scan1: %v", err)
	}
	files1, _ := GetFilesByScanID(ctx, database, scan1.ID)
	if err := UpdateFileHash(ctx, database, files1[0].ID, "scan1hash", time.Now().UTC()); err != nil {
		t.Fatalf("UpdateFileHash scan1: %v", err)
	}

	// Scan 2: different file, same inode (e.g. same file seen in another scan root)
	if err := InsertFile(ctx, database, scan2.ID, "/tmp2/f", 50, 1, 123, &dev); err != nil {
		t.Fatalf("InsertFile scan2: %v", err)
	}

	// HashForInode for scan2 should NOT return scan1's hash (same-scan only)
	got, err := HashForInode(ctx, database, scan2.ID, 123, &dev)
	if err != nil {
		t.Fatalf("HashForInode: %v", err)
	}
	if got != "" {
		t.Errorf("HashForInode(scan2, 123) = %q, want empty (no file in scan2 has that inode hashed yet)", got)
	}
}
