package db

import (
	"context"
	"testing"
)

func TestClaimNextHashJob_onlyReturnsFilesInSameSizeGroups(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	scan, err := CreateScan(ctx, db, "/tmp")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}
	// Two files size 100 (candidates), one size 200, one size 300 (unique — never candidates).
	if err := InsertFile(ctx, db, scan.ID, "/tmp/a", 100, 1, 1, nil); err != nil {
		t.Fatalf("InsertFile a: %v", err)
	}
	if err := InsertFile(ctx, db, scan.ID, "/tmp/b", 100, 2, 2, nil); err != nil {
		t.Fatalf("InsertFile b: %v", err)
	}
	if err := InsertFile(ctx, db, scan.ID, "/tmp/c", 200, 3, 3, nil); err != nil {
		t.Fatalf("InsertFile c: %v", err)
	}
	if err := InsertFile(ctx, db, scan.ID, "/tmp/d", 300, 4, 4, nil); err != nil {
		t.Fatalf("InsertFile d: %v", err)
	}

	// First claim: should return one of the size-100 files (priority order size DESC, so 100 is the only duplicate group).
	f1, err := ClaimNextHashJob(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob 1: %v", err)
	}
	if f1 == nil {
		t.Fatal("ClaimNextHashJob 1: want one file, got nil")
	}
	if f1.Size != 100 {
		t.Errorf("first claim Size = %d, want 100 (only duplicate group)", f1.Size)
	}
	if f1.HashStatus != "hashing" {
		t.Errorf("first claim HashStatus = %q, want hashing", f1.HashStatus)
	}

	// Second claim: the other size-100 file.
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

	// No more candidates (200 and 300 are unique size).
	f3, err := ClaimNextHashJob(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob 3: %v", err)
	}
	if f3 != nil {
		t.Errorf("ClaimNextHashJob 3: want nil (no more jobs), got file id=%d size=%d", f3.ID, f3.Size)
	}
}

func TestClaimNextHashJob_afterOneDoneOtherInGroupStillCandidate(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	scan, err := CreateScan(ctx, db, "/tmp")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}
	if err := InsertFile(ctx, db, scan.ID, "/tmp/a", 100, 1, 1, nil); err != nil {
		t.Fatalf("InsertFile a: %v", err)
	}
	if err := InsertFile(ctx, db, scan.ID, "/tmp/b", 100, 2, 2, nil); err != nil {
		t.Fatalf("InsertFile b: %v", err)
	}

	// Set one file to done manually (simulating a completed hash).
	_, err = db.ExecContext(ctx, "UPDATE files SET hash_status = 'done', hash = 'abc', hashed_at = datetime('now') WHERE path = ?", "/tmp/a")
	if err != nil {
		t.Fatalf("UPDATE done: %v", err)
	}

	// Claim should return the other file (still pending) in the same size group.
	f, err := ClaimNextHashJob(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob: %v", err)
	}
	if f == nil {
		t.Fatal("ClaimNextHashJob: want one file (b still pending), got nil")
	}
	if f.Path != "/tmp/b" {
		t.Errorf("claimed path = %q, want /tmp/b", f.Path)
	}
	if f.Size != 100 {
		t.Errorf("claimed size = %d, want 100", f.Size)
	}
}

func TestClaimNextHashJob_setsStatusToHashingAndDoesNotReturnSameRowTwice(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	scan, err := CreateScan(ctx, db, "/tmp")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}
	if err := InsertFile(ctx, db, scan.ID, "/tmp/a", 100, 1, 1, nil); err != nil {
		t.Fatalf("InsertFile a: %v", err)
	}
	if err := InsertFile(ctx, db, scan.ID, "/tmp/b", 100, 2, 2, nil); err != nil {
		t.Fatalf("InsertFile b: %v", err)
	}

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

	// Verify in DB that the row is now hashing.
	var status string
	err = db.QueryRowContext(ctx, "SELECT hash_status FROM files WHERE id = ?", f1.ID).Scan(&status)
	if err != nil {
		t.Fatalf("SELECT hash_status: %v", err)
	}
	if status != "hashing" {
		t.Errorf("DB hash_status = %q, want hashing", status)
	}

	// Second claim must not return the same row.
	f2, err := ClaimNextHashJob(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("ClaimNextHashJob 2: %v", err)
	}
	if f2 == nil {
		t.Fatal("second claim: want file (the other one), got nil")
	}
	if f2.ID == f1.ID {
		t.Error("second claim returned same file as first")
	}
}
