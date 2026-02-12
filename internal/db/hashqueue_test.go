package db

import (
	"context"
	"testing"
	"time"
)

func TestClaimNextHashJob_onlyReturnsFilesInSameSizeGroups(t *testing.T) {
	db := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	for _, p := range []struct{ path string; size int64; inode int64 }{
		{"a", 100, 1}, {"b", 100, 2}, {"c", 200, 3}, {"d", 300, 4},
	} {
		fileID, err := UpsertFile(ctx, db, folderID, p.path, p.size, 0, p.inode, nil)
		if err != nil {
			t.Fatalf("UpsertFile: %v", err)
		}
		if err := InsertFileScan(ctx, db, fileID, scan.ID); err != nil {
			t.Fatalf("InsertFileScan: %v", err)
		}
	}

	f1, err := ClaimNextHashJob(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob 1: %v", err)
	}
	if f1 == nil {
		t.Fatal("ClaimNextHashJob 1: want one file, got nil")
	}
	if f1.Size != 100 {
		t.Errorf("first claim Size = %d, want 100", f1.Size)
	}
	if f1.HashStatus != "hashing" {
		t.Errorf("first claim HashStatus = %q, want hashing", f1.HashStatus)
	}

	f2, err := ClaimNextHashJob(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob 2: %v", err)
	}
	if f2 == nil {
		t.Fatal("ClaimNextHashJob 2: want one file, got nil")
	}
	if f2.Size != 100 {
		t.Errorf("second claim Size = %d, want 100", f2.Size)
	}
	if f1.ID == f2.ID {
		t.Error("second claim returned same file as first")
	}

	f3, err := ClaimNextHashJob(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob 3: %v", err)
	}
	if f3 != nil {
		t.Errorf("ClaimNextHashJob 3: want nil, got file id=%d", f3.ID)
	}
}

func TestClaimNextHashJob_afterOneDoneOtherInGroupStillCandidate(t *testing.T) {
	db := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	fileID1, _ := UpsertFile(ctx, db, folderID, "a", 100, 1, 1, nil)
	InsertFileScan(ctx, db, fileID1, scan.ID)
	fileID2, _ := UpsertFile(ctx, db, folderID, "b", 100, 2, 2, nil)
	InsertFileScan(ctx, db, fileID2, scan.ID)

	_ = UpdateFileHash(ctx, db, fileID1, "abc", time.Now().UTC())

	f, err := ClaimNextHashJob(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob: %v", err)
	}
	if f == nil {
		t.Fatal("ClaimNextHashJob: want one file (b still pending), got nil")
	}
	if f.Path != "/tmp/b" && f.Path != "b" {
		t.Errorf("claimed path = %q", f.Path)
	}
	if f.Size != 100 {
		t.Errorf("claimed size = %d, want 100", f.Size)
	}
}

// TestClaimNextHashJob_crossScanSameSizeUniquePerScan ensures that when two scans each have
// exactly one file of the same size (no same-size pair within either scan), the second scan's
// hash phase still queues that file because the size appears in another scan.
func TestClaimNextHashJob_crossScanSameSizeUniquePerScan(t *testing.T) {
	db := TestPostgresDB(t)
	ctx := context.Background()

	folder1, _ := AddFolder(ctx, db, "/folder1")
	folder2, _ := AddFolder(ctx, db, "/folder2")
	scan1, _ := CreateScan(ctx, db, folder1)
	scan2, _ := CreateScan(ctx, db, folder2)

	// One file of size 1000 in each folder (unique per scan).
	file1, _ := UpsertFile(ctx, db, folder1, "only", 1000, 0, 1, nil)
	InsertFileScan(ctx, db, file1, scan1.ID)
	file2, _ := UpsertFile(ctx, db, folder2, "only", 1000, 0, 2, nil)
	InsertFileScan(ctx, db, file2, scan2.ID)

	// Hash phase for scan2: file in scan2 should be a candidate because size 1000 exists in scan1.
	f, err := ClaimNextHashJob(ctx, db, scan2.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob: %v", err)
	}
	if f == nil {
		t.Fatal("ClaimNextHashJob: want one file (scan2's file — same size as in scan1), got nil")
	}
	if f.Size != 1000 {
		t.Errorf("claimed size = %d, want 1000", f.Size)
	}
	if f.ID != file2 {
		t.Errorf("claimed file id = %d, want %d", f.ID, file2)
	}
}

func TestClaimNextHashJob_setsStatusToHashingAndDoesNotReturnSameRowTwice(t *testing.T) {
	db := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	fileID1, _ := UpsertFile(ctx, db, folderID, "a", 100, 1, 1, nil)
	InsertFileScan(ctx, db, fileID1, scan.ID)
	fileID2, _ := UpsertFile(ctx, db, folderID, "b", 100, 2, 2, nil)
	InsertFileScan(ctx, db, fileID2, scan.ID)

	f1, err := ClaimNextHashJob(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob 1: %v", err)
	}
	if f1 == nil {
		t.Fatal("first claim: want file, got nil")
	}
	if f1.HashStatus != "hashing" {
		t.Errorf("returned file HashStatus = %q, want hashing", f1.HashStatus)
	}

	var status string
	err = db.QueryRowContext(ctx, "SELECT hash_status FROM files WHERE id = $1", f1.ID).Scan(&status)
	if err != nil {
		t.Fatalf("SELECT hash_status: %v", err)
	}
	if status != "hashing" {
		t.Errorf("DB hash_status = %q, want hashing", status)
	}

	f2, err := ClaimNextHashJob(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob 2: %v", err)
	}
	if f2 == nil {
		t.Fatal("second claim: want file, got nil")
	}
	if f2.ID == f1.ID {
		t.Error("second claim returned same file as first")
	}
}
