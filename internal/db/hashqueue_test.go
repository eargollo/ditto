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
		fileID, err := UpsertFile(ctx, db, folderID, p.path, p.size, 0, ptrInt64(p.inode), nil)
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
	fileID1, _ := UpsertFile(ctx, db, folderID, "a", 100, 1, ptrInt64(1), nil)
	InsertFileScan(ctx, db, fileID1, scan.ID)
	fileID2, _ := UpsertFile(ctx, db, folderID, "b", 100, 2, ptrInt64(2), nil)
	InsertFileScan(ctx, db, fileID2, scan.ID)

	_ = UpdateFileHash(ctx, db, fileID1, "abc", time.Now().UTC(), 0)

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
// exactly one file of the same size (no same-size pair within either scan), the global hash
// queue still queues those files because the size appears in more than one distinct file.
func TestClaimNextHashJob_crossScanSameSizeUniquePerScan(t *testing.T) {
	db := TestPostgresDB(t)
	ctx := context.Background()

	folder1, _ := AddFolder(ctx, db, "/folder1")
	folder2, _ := AddFolder(ctx, db, "/folder2")
	scan1, _ := CreateScan(ctx, db, folder1)
	scan2, _ := CreateScan(ctx, db, folder2)

	// One file of size 1000 in each folder (unique per scan; duplicate size globally).
	file1, _ := UpsertFile(ctx, db, folder1, "only", 1000, 0, ptrInt64(1), nil)
	InsertFileScan(ctx, db, file1, scan1.ID)
	file2, _ := UpsertFile(ctx, db, folder2, "only", 1000, 0, ptrInt64(2), nil)
	InsertFileScan(ctx, db, file2, scan2.ID)

	// Global queue: both files are candidates (size 1000 has 2 distinct files). Claim one.
	f, err := ClaimNextHashJobGlobal(ctx, db)
	if err != nil {
		t.Fatalf("ClaimNextHashJobGlobal: %v", err)
	}
	if f == nil {
		t.Fatal("ClaimNextHashJobGlobal: want one file (same size across scans), got nil")
	}
	if f.Size != 1000 {
		t.Errorf("claimed size = %d, want 1000", f.Size)
	}
	if f.ID != file1 && f.ID != file2 {
		t.Errorf("claimed file id = %d, want %d or %d", f.ID, file1, file2)
	}
}

func TestClaimNextHashJob_setsStatusToHashingAndDoesNotReturnSameRowTwice(t *testing.T) {
	db := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	fileID1, _ := UpsertFile(ctx, db, folderID, "a", 100, 1, ptrInt64(1), nil)
	InsertFileScan(ctx, db, fileID1, scan.ID)
	fileID2, _ := UpsertFile(ctx, db, folderID, "b", 100, 2, ptrInt64(2), nil)
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

// TestRescanSameFiles_doesNotHashAgain scans the same files twice (same paths, all with duplicate sizes so they would be candidates).
// After the first scan we hash them (set done). On the second scan we re-upsert the same paths and assert we do not queue them for hashing.
func TestRescanSameFiles_doesNotHashAgain(t *testing.T) {
	db := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, db, "/data")
	scan1, _ := CreateScan(ctx, db, folderID)

	// First scan: 4 files, two sizes (100 and 200) so both are candidate sizes.
	for _, p := range []struct{ path string; size int64; inode int64 }{
		{"f1", 100, 1}, {"f2", 100, 2}, {"f3", 200, 3}, {"f4", 200, 4},
	} {
		fileID, err := UpsertFile(ctx, db, folderID, p.path, p.size, 0, ptrInt64(p.inode), nil)
		if err != nil {
			t.Fatalf("UpsertFile: %v", err)
		}
		if err := InsertFileScan(ctx, db, fileID, scan1.ID); err != nil {
			t.Fatalf("InsertFileScan: %v", err)
		}
		_ = UpdateFileHash(ctx, db, fileID, "hash-"+p.path, time.Now().UTC(), 0)
	}

	// Second scan: same folder, same paths (re-scan). Upsert updates the same file rows and does not overwrite hash_status.
	scan2, _ := CreateScan(ctx, db, folderID)
	for _, p := range []struct{ path string; size int64; inode int64 }{
		{"f1", 100, 1}, {"f2", 100, 2}, {"f3", 200, 3}, {"f4", 200, 4},
	} {
		fileID, err := UpsertFile(ctx, db, folderID, p.path, p.size, 0, ptrInt64(p.inode), nil)
		if err != nil {
			t.Fatalf("UpsertFile scan2: %v", err)
		}
		if err := InsertFileScan(ctx, db, fileID, scan2.ID); err != nil {
			t.Fatalf("InsertFileScan scan2: %v", err)
		}
	}

	n, err := CountHashCandidates(ctx, db, scan2.ID)
	if err != nil {
		t.Fatalf("CountHashCandidates: %v", err)
	}
	if n != 0 {
		t.Errorf("second scan: CountHashCandidates = %d, want 0 (same files already done, should not re-hash)", n)
	}

	f, err := ClaimNextHashJob(ctx, db, scan2.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob: %v", err)
	}
	if f != nil {
		t.Errorf("second scan: ClaimNextHashJob = file id=%d, want nil (no file should be queued)", f.ID)
	}
}
