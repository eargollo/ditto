package db

import (
	"context"
	"testing"
)

// TestGetFileByID_found verifies GetFileByID returns the file with full path and hash when it exists and is not deleted.
func TestGetFileByID_found(t *testing.T) {
	ctx := context.Background()
	q := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, q, "/tmp")
	fileID, err := UpsertFile(ctx, q, folderID, "foo", 100, 1707292800, nil, nil)
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	_, err = q.ExecContext(ctx, "UPDATE files SET hash = 'abc123', hash_status = 'done', hashed_at = (NOW() AT TIME ZONE 'UTC'), hashed_mtime = 1707292800 WHERE id = $1", fileID)
	if err != nil {
		t.Fatalf("UPDATE hash: %v", err)
	}

	f, err := GetFileByID(ctx, q, fileID)
	if err != nil {
		t.Fatalf("GetFileByID: %v", err)
	}
	if f == nil {
		t.Fatal("GetFileByID: got nil, want file")
	}
	if f.ID != fileID || f.Path != "/tmp/foo" || f.Size != 100 {
		t.Errorf("GetFileByID: id=%d path=%q size=%d, want id=%d path=/tmp/foo size=100", f.ID, f.Path, f.Size, fileID)
	}
	if f.Hash == nil || *f.Hash != "abc123" {
		t.Errorf("GetFileByID: hash=%v, want abc123", f.Hash)
	}
	if f.HashStatus != "done" {
		t.Errorf("GetFileByID: hash_status=%q, want done", f.HashStatus)
	}
	if f.FolderID != folderID {
		t.Errorf("GetFileByID: folder_id=%d, want %d", f.FolderID, folderID)
	}
}

// TestGetFileByID_notFound verifies GetFileByID returns (nil, nil) for non-existent ID.
func TestGetFileByID_notFound(t *testing.T) {
	ctx := context.Background()
	q := TestPostgresDB(t)

	f, err := GetFileByID(ctx, q, 99999)
	if err != nil {
		t.Fatalf("GetFileByID: %v", err)
	}
	if f != nil {
		t.Errorf("GetFileByID: got %+v, want nil", f)
	}
}

// TestGetFileByID_deletedReturnsNil verifies GetFileByID returns (nil, nil) when the file has deleted_at set.
func TestGetFileByID_deletedReturnsNil(t *testing.T) {
	ctx := context.Background()
	q := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, q, "/tmp")
	fileID, _ := UpsertFile(ctx, q, folderID, "gone", 10, 100, nil, nil)
	_, _ = q.ExecContext(ctx, "UPDATE files SET hash = 'x', hash_status = 'done', deleted_at = (NOW() AT TIME ZONE 'UTC') WHERE id = $1", fileID)

	f, err := GetFileByID(ctx, q, fileID)
	if err != nil {
		t.Fatalf("GetFileByID: %v", err)
	}
	if f != nil {
		t.Errorf("GetFileByID (deleted file): got %+v, want nil", f)
	}
}

func ptrInt64(n int64) *int64 { v := n; return &v }

func TestUpsertFile_InsertFileScan_and_GetFilesByScanID(t *testing.T) {
	db := TestPostgresDB(t)
	ctx := context.Background()

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	deviceID := int64(42)
	fileID, err := UpsertFile(ctx, db, folderID, "foo", 100, 1707292800, ptrInt64(12345), &deviceID)
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
	if f.Size != 100 || f.MTime != 1707292800 || f.Inode == nil || *f.Inode != 12345 {
		t.Errorf("file: size=%d mtime=%d inode=%v", f.Size, f.MTime, f.Inode)
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
	fileID1, _ := UpsertFile(ctx, db, folderID, "a", 10, 100, ptrInt64(1), nil)
	InsertFileScan(ctx, db, fileID1, scan.ID)
	fileID2, _ := UpsertFile(ctx, db, folderID, "b", 20, 200, ptrInt64(2), nil)
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
		{Path: "a", Size: 10, MTime: 100, Inode: ptrInt64(1), DeviceID: &dev},
		{Path: "b", Size: 20, MTime: 200, Inode: ptrInt64(2), DeviceID: nil},
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

func TestUpdateFilesDeletedAtForScan(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	// File a and b in scan; file c not in scan (same folder)
	fileIDa, _ := UpsertFile(ctx, db, folderID, "a", 10, 0, ptrInt64(1), nil)
	fileIDb, _ := UpsertFile(ctx, db, folderID, "b", 20, 0, ptrInt64(2), nil)
	fileIDc, _ := UpsertFile(ctx, db, folderID, "c", 30, 0, ptrInt64(3), nil)
	InsertFileScan(ctx, db, fileIDa, scan.ID)
	InsertFileScan(ctx, db, fileIDb, scan.ID)
	// c is not in file_scan for this scan

	if err := UpdateScanCompletedAt(ctx, db, scan.ID, 2, 0); err != nil {
		t.Fatalf("UpdateScanCompletedAt: %v", err)
	}
	if err := UpdateFilesDeletedAtForScan(ctx, db, scan.ID, folderID); err != nil {
		t.Fatalf("UpdateFilesDeletedAtForScan: %v", err)
	}

	var deletedA, deletedB, deletedC interface{}
	db.QueryRowContext(ctx, "SELECT deleted_at FROM files WHERE id = $1", fileIDa).Scan(&deletedA)
	db.QueryRowContext(ctx, "SELECT deleted_at FROM files WHERE id = $1", fileIDb).Scan(&deletedB)
	db.QueryRowContext(ctx, "SELECT deleted_at FROM files WHERE id = $1", fileIDc).Scan(&deletedC)
	if deletedA != nil {
		t.Errorf("file a (in scan) should have deleted_at NULL, got %v", deletedA)
	}
	if deletedB != nil {
		t.Errorf("file b (in scan) should have deleted_at NULL, got %v", deletedB)
	}
	if deletedC == nil {
		t.Errorf("file c (not in scan) should have deleted_at set, got nil")
	}
}

// TestUpdateFilesDeletedAtForScan_undelete ensures a file that had deleted_at set (e.g. was
// missing in a previous scan) gets deleted_at = NULL when it appears in the current scan.
func TestUpdateFilesDeletedAtForScan_undelete(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	fileIDa, _ := UpsertFile(ctx, db, folderID, "a", 10, 1000, ptrInt64(1), nil)
	InsertFileScan(ctx, db, fileIDa, scan.ID)

	// Simulate "file was deleted in a previous scan": set deleted_at.
	if _, err := db.ExecContext(ctx, "UPDATE files SET deleted_at = (NOW() AT TIME ZONE 'UTC') WHERE id = $1", fileIDa); err != nil {
		t.Fatalf("set deleted_at: %v", err)
	}

	if err := UpdateScanCompletedAt(ctx, db, scan.ID, 1, 0); err != nil {
		t.Fatalf("UpdateScanCompletedAt: %v", err)
	}
	if err := UpdateFilesDeletedAtForScan(ctx, db, scan.ID, folderID); err != nil {
		t.Fatalf("UpdateFilesDeletedAtForScan: %v", err)
	}

	var deletedAt interface{}
	db.QueryRowContext(ctx, "SELECT deleted_at FROM files WHERE id = $1", fileIDa).Scan(&deletedAt)
	if deletedAt != nil {
		t.Errorf("file a (in scan, was deleted) should have deleted_at NULL after update, got %v", deletedAt)
	}
}

// TestUpdateFilesDeletedAtForScan_hashResetWhenMtimeDiffers ensures a file in the scan whose
// mtime differs from hashed_mtime (or never hashed) gets hash_status = 'pending' and hash
// fields cleared so it is re-hashed.
func TestUpdateFilesDeletedAtForScan_hashResetWhenMtimeDiffers(t *testing.T) {
	ctx := context.Background()
	db := TestPostgresDB(t)

	folderID, _ := AddFolder(ctx, db, "/tmp")
	scan, _ := CreateScan(ctx, db, folderID)
	fileIDa, _ := UpsertFile(ctx, db, folderID, "a", 10, 1000, ptrInt64(1), nil)
	InsertFileScan(ctx, db, fileIDa, scan.ID)

	// Set hash state as if we had hashed the file when mtime was 999; now mtime is 1000 (file changed).
	if _, err := db.ExecContext(ctx,
		"UPDATE files SET hash = 'oldhash', hash_status = 'done', hashed_at = (NOW() AT TIME ZONE 'UTC'), hashed_mtime = 999 WHERE id = $1",
		fileIDa); err != nil {
		t.Fatalf("set hash state: %v", err)
	}

	if err := UpdateScanCompletedAt(ctx, db, scan.ID, 1, 0); err != nil {
		t.Fatalf("UpdateScanCompletedAt: %v", err)
	}
	if err := UpdateFilesDeletedAtForScan(ctx, db, scan.ID, folderID); err != nil {
		t.Fatalf("UpdateFilesDeletedAtForScan: %v", err)
	}

	var hashStatus string
	var hash, hashedAt, hashedMtime interface{}
	db.QueryRowContext(ctx, "SELECT hash_status, hash, hashed_at, hashed_mtime FROM files WHERE id = $1", fileIDa).Scan(&hashStatus, &hash, &hashedAt, &hashedMtime)
	if hashStatus != "pending" {
		t.Errorf("file a hash_status = %q, want pending", hashStatus)
	}
	if hash != nil {
		t.Errorf("file a hash should be NULL after mtime change, got %v", hash)
	}
	if hashedAt != nil {
		t.Errorf("file a hashed_at should be NULL after mtime change, got %v", hashedAt)
	}
	if hashedMtime != nil {
		t.Errorf("file a hashed_mtime should be NULL after mtime change, got %v", hashedMtime)
	}
}
