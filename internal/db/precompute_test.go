package db

import (
	"context"
	"testing"
	"time"
)

func TestPrecomputeDuplicateGroupsHash_and_Summary_and_FilesInHashGroupByHash(t *testing.T) {
	ctx := context.Background()
	database := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, database, "/tmp")
	scan, _ := CreateScan(ctx, database, folderID)
	fileID1, _ := UpsertFile(ctx, database, folderID, "a", 100, 0, ptrInt64(1), nil)
	fileID2, _ := UpsertFile(ctx, database, folderID, "b", 100, 0, ptrInt64(2), nil)
	InsertFileScan(ctx, database, fileID1, scan.ID)
	InsertFileScan(ctx, database, fileID2, scan.ID)
	hash := "abc123"
	now := time.Now().UTC()
	_ = UpdateFileHash(ctx, database, fileID1, hash, now, 0)
	_ = UpdateFileHash(ctx, database, fileID2, hash, now, 0)
	_ = UpdateScanCompletedAt(ctx, database, scan.ID, 2, 0)
	_ = UpdateFilesDeletedAtForScan(ctx, database, scan.ID, folderID)

	if err := PrecomputeDuplicateGroupsHash(ctx, database); err != nil {
		t.Fatalf("PrecomputeDuplicateGroupsHash: %v", err)
	}

	summary, err := GetDuplicateGroupsHashSummary(ctx, database)
	if err != nil {
		t.Fatalf("GetDuplicateGroupsHashSummary: %v", err)
	}
	if summary.GroupCount != 1 || summary.TotalFiles != 2 || summary.TotalSize != 200 {
		t.Errorf("summary = %+v, want GroupCount=1 TotalFiles=2 TotalSize=200", summary)
	}
	if summary.ReclaimableSize != 100 {
		t.Errorf("ReclaimableSize = %d, want 100 (one copy can be removed)", summary.ReclaimableSize)
	}

	n, err := DuplicateGroupsHashCount(ctx, database)
	if err != nil {
		t.Fatalf("DuplicateGroupsHashCount: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}

	groups, err := DuplicateGroupsHashPaginated(ctx, database, 10, 0)
	if err != nil {
		t.Fatalf("DuplicateGroupsHashPaginated: %v", err)
	}
	if len(groups) != 1 || groups[0].Hash != hash || groups[0].Count != 2 || groups[0].Size != 200 {
		t.Errorf("groups = %+v", groups)
	}

	files, err := FilesInHashGroupByHash(ctx, database, hash, 0)
	if err != nil {
		t.Fatalf("FilesInHashGroupByHash: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("FilesInHashGroupByHash len = %d, want 2", len(files))
	}
	filesLimit, err := FilesInHashGroupByHash(ctx, database, hash, 1)
	if err != nil {
		t.Fatalf("FilesInHashGroupByHash limit 1: %v", err)
	}
	if len(filesLimit) != 1 {
		t.Errorf("FilesInHashGroupByHash(limit=1) len = %d, want 1", len(filesLimit))
	}
}

func TestFilesInHashGroupByHash_excludes_deleted_at(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	fileID1, _ := UpsertFile(ctx, db, folderID, "a", 100, 0, ptrInt64(1), nil)
	fileID2, _ := UpsertFile(ctx, db, folderID, "b", 100, 0, ptrInt64(2), nil)
	InsertFileScan(ctx, db, fileID1, scan.ID)
	InsertFileScan(ctx, db, fileID2, scan.ID)
	hash := "same"
	now := time.Now().UTC()
	_ = UpdateFileHash(ctx, db, fileID1, hash, now, 0)
	_ = UpdateFileHash(ctx, db, fileID2, hash, now, 0)
	_ = UpdateScanCompletedAt(ctx, db, scan.ID, 2, 0)
	_ = UpdateFilesDeletedAtForScan(ctx, db, scan.ID, folderID)

	// Both files in scan, so both deleted_at NULL. Add a third file with same hash but not in scan (simulate deleted).
	fileID3, _ := UpsertFile(ctx, db, folderID, "c", 100, 0, ptrInt64(3), nil)
	_ = UpdateFileHash(ctx, db, fileID3, hash, now, 0)
	// Do not insert fileID3 into file_scan; run UpdateFilesDeletedAtForScan again so fileID3 gets deleted_at set
	_ = UpdateFilesDeletedAtForScan(ctx, db, scan.ID, folderID)

	files, err := FilesInHashGroupByHash(ctx, db, hash, 0)
	if err != nil {
		t.Fatalf("FilesInHashGroupByHash: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("len(files) = %d, want 2 (file c should be excluded by deleted_at)", len(files))
	}
}

func TestPrecomputeDuplicateGroupsHash_empty_when_no_duplicates(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	fileID, _ := UpsertFile(ctx, db, folderID, "a", 100, 0, ptrInt64(1), nil)
	InsertFileScan(ctx, db, fileID, scan.ID)
	now := time.Now().UTC()
	_ = UpdateFileHash(ctx, db, fileID, "unique", now, 0)
	_ = UpdateScanCompletedAt(ctx, db, scan.ID, 1, 0)
	_ = UpdateFilesDeletedAtForScan(ctx, db, scan.ID, folderID)

	if err := PrecomputeDuplicateGroupsHash(ctx, db); err != nil {
		t.Fatalf("PrecomputeDuplicateGroupsHash: %v", err)
	}
	summary, _ := GetDuplicateGroupsHashSummary(ctx, db)
	if summary.GroupCount != 0 || summary.TotalFiles != 0 {
		t.Errorf("summary = %+v, want zeros", summary)
	}
	groups, _ := DuplicateGroupsHashPaginated(ctx, db, 10, 0)
	if len(groups) != 0 {
		t.Errorf("groups = %v, want empty", groups)
	}
}

func TestRefreshDuplicateGroupsForHashes_empty_noop(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)
	if err := RefreshDuplicateGroupsForHashes(ctx, db, nil); err != nil {
		t.Fatalf("RefreshDuplicateGroupsForHashes(nil): %v", err)
	}
	if err := RefreshDuplicateGroupsForHashes(ctx, db, []string{}); err != nil {
		t.Fatalf("RefreshDuplicateGroupsForHashes(empty): %v", err)
	}
}

func TestRefreshDuplicateGroupsForHashes_one_group_insert(t *testing.T) {
	ctx := context.Background()
	database := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, database, "/tmp")
	scan, _ := CreateScan(ctx, database, folderID)
	fileID1, _ := UpsertFile(ctx, database, folderID, "a", 100, 0, ptrInt64(1), nil)
	fileID2, _ := UpsertFile(ctx, database, folderID, "b", 100, 0, ptrInt64(2), nil)
	InsertFileScan(ctx, database, fileID1, scan.ID)
	InsertFileScan(ctx, database, fileID2, scan.ID)
	hash := "dupHash"
	now := time.Now().UTC()
	_ = UpdateFileHash(ctx, database, fileID1, hash, now, 0)
	_ = UpdateFileHash(ctx, database, fileID2, hash, now, 0)
	_ = UpdateScanCompletedAt(ctx, database, scan.ID, 2, 0)
	_ = UpdateFilesDeletedAtForScan(ctx, database, scan.ID, folderID)

	if err := RefreshDuplicateGroupsForHashes(ctx, database, []string{hash}); err != nil {
		t.Fatalf("RefreshDuplicateGroupsForHashes: %v", err)
	}
	summary, _ := GetDuplicateGroupsHashSummary(ctx, database)
	if summary.GroupCount != 1 || summary.TotalFiles != 2 || summary.TotalSize != 200 || summary.ReclaimableSize != 100 {
		t.Errorf("summary = %+v, want one group, 2 files, 200 size, 100 reclaimable", summary)
	}
	n, _ := DuplicateGroupsHashCount(ctx, database)
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
}

func TestRefreshDuplicateGroupsForHashes_single_file_removes_row(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan1, _ := CreateScan(ctx, db, folderID)
	fileID1, _ := UpsertFile(ctx, db, folderID, "a", 100, 0, ptrInt64(1), nil)
	fileID2, _ := UpsertFile(ctx, db, folderID, "b", 100, 0, ptrInt64(2), nil)
	InsertFileScan(ctx, db, fileID1, scan1.ID)
	InsertFileScan(ctx, db, fileID2, scan1.ID)
	hash := "onlyTwo"
	now := time.Now().UTC()
	_ = UpdateFileHash(ctx, db, fileID1, hash, now, 0)
	_ = UpdateFileHash(ctx, db, fileID2, hash, now, 0)
	_ = UpdateScanCompletedAt(ctx, db, scan1.ID, 2, 0)
	_ = UpdateFilesDeletedAtForScan(ctx, db, scan1.ID, folderID)

	// Seed table with one group (a and b both live)
	if err := RefreshDuplicateGroupsForHashes(ctx, db, []string{hash}); err != nil {
		t.Fatalf("RefreshDuplicateGroupsForHashes: %v", err)
	}
	if n, _ := DuplicateGroupsHashCount(ctx, db); n != 1 {
		t.Fatalf("after seed: count = %d, want 1", n)
	}
	// Second scan that only includes "a" (b disappeared from disk). b gets deleted_at.
	scan2, _ := CreateScan(ctx, db, folderID)
	InsertFileScan(ctx, db, fileID1, scan2.ID)
	_ = UpdateScanCompletedAt(ctx, db, scan2.ID, 1, 0)
	_ = UpdateFilesDeletedAtNotInScan(ctx, db, scan2.ID, folderID)
	affected, _ := DistinctHashesWithDeletedInFolder(ctx, db, folderID)
	if err := RefreshDuplicateGroupsForHashes(ctx, db, affected); err != nil {
		t.Fatalf("RefreshDuplicateGroupsForHashes after delete: %v", err)
	}
	// Group should be gone (only one live file left for that hash)
	n, _ := DuplicateGroupsHashCount(ctx, db)
	if n != 0 {
		t.Errorf("after one file deleted: count = %d, want 0", n)
	}
}

func TestRefreshDuplicateGroupsForHashes_other_hashes_untouched(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	f1a, _ := UpsertFile(ctx, db, folderID, "f1a", 100, 0, ptrInt64(1), nil)
	f1b, _ := UpsertFile(ctx, db, folderID, "f1b", 100, 0, ptrInt64(2), nil)
	f2a, _ := UpsertFile(ctx, db, folderID, "f2a", 200, 0, ptrInt64(3), nil)
	f2b, _ := UpsertFile(ctx, db, folderID, "f2b", 200, 0, ptrInt64(4), nil)
	InsertFileScan(ctx, db, f1a, scan.ID)
	InsertFileScan(ctx, db, f1b, scan.ID)
	InsertFileScan(ctx, db, f2a, scan.ID)
	InsertFileScan(ctx, db, f2b, scan.ID)
	now := time.Now().UTC()
	hash1, hash2 := "h1", "h2"
	_ = UpdateFileHash(ctx, db, f1a, hash1, now, 0)
	_ = UpdateFileHash(ctx, db, f1b, hash1, now, 0)
	_ = UpdateFileHash(ctx, db, f2a, hash2, now, 0)
	_ = UpdateFileHash(ctx, db, f2b, hash2, now, 0)
	_ = UpdateScanCompletedAt(ctx, db, scan.ID, 4, 0)
	_ = UpdateFilesDeletedAtForScan(ctx, db, scan.ID, folderID)

	// Refresh only hash1
	if err := RefreshDuplicateGroupsForHashes(ctx, db, []string{hash1}); err != nil {
		t.Fatalf("RefreshDuplicateGroupsForHashes(h1): %v", err)
	}
	// Only one group so far
	if n, _ := DuplicateGroupsHashCount(ctx, db); n != 1 {
		t.Fatalf("after refresh h1: count = %d, want 1", n)
	}
	// Refresh hash2
	if err := RefreshDuplicateGroupsForHashes(ctx, db, []string{hash2}); err != nil {
		t.Fatalf("RefreshDuplicateGroupsForHashes(h2): %v", err)
	}
	summary, _ := GetDuplicateGroupsHashSummary(ctx, db)
	if summary.GroupCount != 2 || summary.TotalFiles != 4 || summary.TotalSize != 600 {
		t.Errorf("summary = %+v, want 2 groups, 4 files, 600 size", summary)
	}
	// Refresh only hash1 again (e.g. one file in h1 changed); h2 row must remain
	if err := RefreshDuplicateGroupsForHashes(ctx, db, []string{hash1}); err != nil {
		t.Fatalf("RefreshDuplicateGroupsForHashes(h1 again): %v", err)
	}
	groups, _ := DuplicateGroupsHashPaginated(ctx, db, 10, 0)
	if len(groups) != 2 {
		t.Errorf("groups len = %d, want 2 (other hash untouched)", len(groups))
	}
}
