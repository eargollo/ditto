package db

import (
	"context"
	"testing"
	"time"
)

func TestDuplicateGroupsByHash_and_FilesInHashGroup(t *testing.T) {
	ctx := context.Background()
	db, _ := Open(":memory:")
	defer db.Close()
	_ = Migrate(db)

	scan, _ := CreateScan(ctx, db, "/tmp")
	_ = InsertFile(ctx, db, scan.ID, "/tmp/a", 100, 0, 1, nil)
	_ = InsertFile(ctx, db, scan.ID, "/tmp/b", 100, 0, 2, nil)
	hash := "abc123"
	now := time.Now().UTC()
	_ = UpdateFileHash(ctx, db, 1, hash, now)
	_ = UpdateFileHash(ctx, db, 2, hash, now)

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
	db, _ := Open(":memory:")
	defer db.Close()
	_ = Migrate(db)

	scan1, _ := CreateScan(ctx, db, "/folder1")
	scan2, _ := CreateScan(ctx, db, "/folder2")
	hash := "xyz"
	now := time.Now().UTC()
	_ = InsertFile(ctx, db, scan1.ID, "/folder1/a", 50, 0, 1, nil)
	_ = InsertFile(ctx, db, scan1.ID, "/folder1/b", 50, 0, 2, nil)
	_ = InsertFile(ctx, db, scan2.ID, "/folder2/c", 50, 0, 3, nil)
	_ = InsertFile(ctx, db, scan2.ID, "/folder2/d", 50, 0, 4, nil)
	for _, id := range []int64{1, 2, 3, 4} {
		_ = UpdateFileHash(ctx, db, id, hash, now)
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
	// Empty scanIDs returns 0 / nil
	n0, _ := DuplicateGroupsByHashCountAcrossScans(ctx, db, nil)
	if n0 != 0 {
		t.Errorf("count with nil = %d, want 0", n0)
	}
	groups0, _ := DuplicateGroupsByHashPaginatedAcrossScans(ctx, db, nil, 10, 0)
	if groups0 != nil {
		t.Errorf("groups with nil = %v", groups0)
	}
}
