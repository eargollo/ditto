package db

import (
	"context"
	"database/sql"
	"testing"
)

func TestAddScanRoot_and_ListScanRoots(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	id1, err := AddScanRoot(ctx, db, "/tmp/foo")
	if err != nil {
		t.Fatalf("AddScanRoot: %v", err)
	}
	if id1 <= 0 {
		t.Errorf("AddScanRoot id = %d, want positive", id1)
	}

	roots, err := ListScanRoots(ctx, db)
	if err != nil {
		t.Fatalf("ListScanRoots: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("ListScanRoots len = %d, want 1", len(roots))
	}
	if roots[0].ID != id1 || roots[0].Path != "/tmp/foo" {
		t.Errorf("ListScanRoots[0] = %+v", roots[0])
	}

	id2, err := AddScanRoot(ctx, db, "/tmp/bar")
	if err != nil {
		t.Fatalf("AddScanRoot second: %v", err)
	}
	roots, err = ListScanRoots(ctx, db)
	if err != nil {
		t.Fatalf("ListScanRoots: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("ListScanRoots len = %d, want 2", len(roots))
	}
	if roots[0].ID != id1 || roots[1].ID != id2 {
		t.Errorf("ListScanRoots order: got [%d, %d]", roots[0].ID, roots[1].ID)
	}
}

func TestGetScanRoot(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	id, _ := AddScanRoot(ctx, db, "/home/user")
	got, err := GetScanRoot(ctx, db, id)
	if err != nil {
		t.Fatalf("GetScanRoot: %v", err)
	}
	if got.ID != id || got.Path != "/home/user" {
		t.Errorf("GetScanRoot = %+v", got)
	}

	_, err = GetScanRoot(ctx, db, 99999)
	if err != sql.ErrNoRows {
		t.Errorf("GetScanRoot(99999): err = %v, want sql.ErrNoRows", err)
	}
}

func TestDeleteScanRoot(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	id, _ := AddScanRoot(ctx, db, "/x")
	ok, err := DeleteScanRoot(ctx, db, id)
	if err != nil {
		t.Fatalf("DeleteScanRoot: %v", err)
	}
	if !ok {
		t.Error("DeleteScanRoot: want true")
	}
	roots, _ := ListScanRoots(ctx, db)
	if len(roots) != 0 {
		t.Errorf("after delete ListScanRoots len = %d, want 0", len(roots))
	}

	ok, err = DeleteScanRoot(ctx, db, 99999)
	if err != nil {
		t.Fatalf("DeleteScanRoot(99999): %v", err)
	}
	if ok {
		t.Error("DeleteScanRoot(99999): want false")
	}
}
