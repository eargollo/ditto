package db

import (
	"context"
	"testing"
	"time"
)

func TestDuplicateGroupsByHash_and_FilesInHashGroup(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	fileID1, _ := UpsertFile(ctx, db, folderID, "a", 100, 0, 1, nil)
	InsertFileScan(ctx, db, fileID1, scan.ID)
	fileID2, _ := UpsertFile(ctx, db, folderID, "b", 100, 0, 2, nil)
	InsertFileScan(ctx, db, fileID2, scan.ID)
	hash := "abc123"
	now := time.Now().UTC()
	_ = UpdateFileHash(ctx, db, fileID1, hash, now)
	_ = UpdateFileHash(ctx, db, fileID2, hash, now)

	groups, err := DuplicateGroupsByHash(ctx, db, scan.ID)
	if err != nil {
		t.Fatalf("DuplicateGroupsByHash: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1", len(groups))
	}
	if groups[0].Hash != hash || groups[0].Count != 2 || groups[0].Size != 200 {
		t.Errorf("group = %+v", groups[0])
	}

	files, err := FilesInHashGroup(ctx, db, scan.ID, hash)
	if err != nil {
		t.Fatalf("FilesInHashGroup: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
}

func TestDuplicateGroupsByHashAcrossScans(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	folder1ID, _ := AddFolder(ctx, db, "/folder1")
	folder2ID, _ := AddFolder(ctx, db, "/folder2")
	scan1, _ := CreateScan(ctx, db, folder1ID)
	scan2, _ := CreateScan(ctx, db, folder2ID)
	hash := "xyz"
	now := time.Now().UTC()
	for _, pair := range []struct{ folderID int64; path string; size int64; inode int64 }{
		{folder1ID, "a", 50, 1},
		{folder1ID, "b", 50, 2},
		{folder2ID, "c", 50, 3},
		{folder2ID, "d", 50, 4},
	} {
		fileID, _ := UpsertFile(ctx, db, pair.folderID, pair.path, pair.size, 0, pair.inode, nil)
		if pair.folderID == folder1ID {
			InsertFileScan(ctx, db, fileID, scan1.ID)
		} else {
			InsertFileScan(ctx, db, fileID, scan2.ID)
		}
		_ = UpdateFileHash(ctx, db, fileID, hash, now)
	}

	scanIDs := []int64{scan1.ID, scan2.ID}
	n, err := DuplicateGroupsByHashCountAcrossScans(ctx, db, scanIDs)
	if err != nil {
		t.Fatalf("DuplicateGroupsByHashCountAcrossScans: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
	groups, err := DuplicateGroupsByHashPaginatedAcrossScans(ctx, db, scanIDs, 10, 0)
	if err != nil {
		t.Fatalf("DuplicateGroupsByHashPaginatedAcrossScans: %v", err)
	}
	if len(groups) != 1 || groups[0].Hash != hash || groups[0].Count != 4 || groups[0].Size != 200 {
		t.Errorf("groups = %+v", groups)
	}
	files, err := FilesInHashGroupAcrossScans(ctx, db, scanIDs, hash)
	if err != nil {
		t.Fatalf("FilesInHashGroupAcrossScans: %v", err)
	}
	if len(files) != 4 {
		t.Errorf("len(files) = %d, want 4", len(files))
	}
	n0, _ := DuplicateGroupsByHashCountAcrossScans(ctx, db, nil)
	if n0 != 0 {
		t.Errorf("count with nil = %d, want 0", n0)
	}
	groups0, _ := DuplicateGroupsByHashPaginatedAcrossScans(ctx, db, nil, 10, 0)
	if groups0 != nil {
		t.Errorf("groups with nil = %v", groups0)
	}
}
