package scan

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/eargollo/ditto/internal/db"
)

// ScanOptions configures a scan run.
type ScanOptions struct {
	ExcludePatterns   []string
	MaxFilesPerSecond int
}

// RunScan walks rootPath, ensures a folder exists for it, creates a scan, upserts files and ledger rows, then sets the scan's completed_at.
// Uses the parallel pipeline (multiple walkers, batched DB writers). rootPath must be an existing directory. Returns scanID or error.
func RunScan(ctx context.Context, q db.Querier, rootPath string, opts *ScanOptions) (int64, error) {
	rootPath = filepath.Clean(rootPath)
	info, err := os.Stat(rootPath)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, errors.New("root path is not a directory")
	}

	folderID, err := db.GetOrCreateFolderByPath(ctx, q, rootPath)
	if err != nil {
		return 0, err
	}
	folder, err := db.GetFolder(ctx, q, folderID)
	if err != nil {
		return 0, err
	}
	folderPath := folder.Path

	s, err := db.CreateScan(ctx, q, folderID)
	if err != nil {
		return 0, err
	}
	scanID := s.ID

	log.Printf("[scan] started for scan %d path %s (pipeline)", scanID, rootPath)
	fileCount, skippedScan, _, err := RunPipeline(ctx, q, scanID, folderID, rootPath, folderPath, opts, nil)
	if err != nil {
		return 0, err
	}
	if err := db.UpdateScanCompletedAt(ctx, q, scanID, fileCount, skippedScan); err != nil {
		return 0, err
	}
	deletedAtStart := time.Now()
	log.Printf("[scan] starting deleted_at update for scan %d (folder %d, %d files)", scanID, folderID, fileCount)

	log.Printf("[scan] starting deleted_at update (not in scan) for scan %d", scanID)
	t1 := time.Now()
	if err := db.UpdateFilesDeletedAtNotInScan(ctx, q, scanID, folderID); err != nil {
		log.Printf("[scan] deleted_at update (not in scan) failed for scan %d: %v", scanID, err)
		return 0, err
	}
	log.Printf("[scan] deleted_at update (not in scan) for scan %d took %d ms", scanID, time.Since(t1).Milliseconds())

	log.Printf("[scan] starting deleted_at update (in scan, undelete) for scan %d", scanID)
	t2 := time.Now()
	if err := db.UpdateFilesDeletedAtInScanUndelete(ctx, q, scanID); err != nil {
		log.Printf("[scan] deleted_at update (in scan, undelete) failed for scan %d: %v", scanID, err)
		return 0, err
	}
	log.Printf("[scan] deleted_at update (in scan, undelete) for scan %d took %d ms", scanID, time.Since(t2).Milliseconds())

	log.Printf("[scan] starting deleted_at update (in scan, hash reset) for scan %d", scanID)
	t3 := time.Now()
	if err := db.UpdateFilesDeletedAtInScanHashReset(ctx, q, scanID); err != nil {
		log.Printf("[scan] deleted_at update (in scan, hash reset) failed for scan %d: %v", scanID, err)
		return 0, err
	}
	log.Printf("[scan] deleted_at update (in scan, hash reset) for scan %d took %d ms", scanID, time.Since(t3).Milliseconds())

	d := time.Since(deletedAtStart).Milliseconds()
	_ = db.UpdateScanDeletedAtUpdateDuration(ctx, q, scanID, d)
	log.Printf("[scan] deleted_at update for scan %d took %d ms total", scanID, d)
	return scanID, nil
}

// RunScanForExisting walks rootPath and upserts files + ledger for the existing scan (scanID). Use when the scan row was already created.
// Uses the parallel pipeline (multiple walkers, batched DB writers).
func RunScanForExisting(ctx context.Context, q db.Querier, scanID int64, folderID int64, rootPath string, opts *ScanOptions) error {
	rootPath = filepath.Clean(rootPath)
	info, err := os.Stat(rootPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("root path is not a directory")
	}
	folder, err := db.GetFolder(ctx, q, folderID)
	if err != nil {
		return err
	}
	folderPath := folder.Path

	fileCount, skippedScan, _, err := RunPipeline(ctx, q, scanID, folderID, rootPath, folderPath, opts, nil)
	if err != nil {
		return err
	}
	if err := db.UpdateScanCompletedAt(ctx, q, scanID, fileCount, skippedScan); err != nil {
		return err
	}
	deletedAtStart := time.Now()
	log.Printf("[scan] starting deleted_at update for scan %d (folder %d, %d files)", scanID, folderID, fileCount)

	log.Printf("[scan] starting deleted_at update (not in scan) for scan %d", scanID)
	t1 := time.Now()
	if err := db.UpdateFilesDeletedAtNotInScan(ctx, q, scanID, folderID); err != nil {
		log.Printf("[scan] deleted_at update (not in scan) failed for scan %d: %v", scanID, err)
		return err
	}
	log.Printf("[scan] deleted_at update (not in scan) for scan %d took %d ms", scanID, time.Since(t1).Milliseconds())

	log.Printf("[scan] starting deleted_at update (in scan, undelete) for scan %d", scanID)
	t2 := time.Now()
	if err := db.UpdateFilesDeletedAtInScanUndelete(ctx, q, scanID); err != nil {
		log.Printf("[scan] deleted_at update (in scan, undelete) failed for scan %d: %v", scanID, err)
		return err
	}
	log.Printf("[scan] deleted_at update (in scan, undelete) for scan %d took %d ms", scanID, time.Since(t2).Milliseconds())

	log.Printf("[scan] starting deleted_at update (in scan, hash reset) for scan %d", scanID)
	t3 := time.Now()
	if err := db.UpdateFilesDeletedAtInScanHashReset(ctx, q, scanID); err != nil {
		log.Printf("[scan] deleted_at update (in scan, hash reset) failed for scan %d: %v", scanID, err)
		return err
	}
	log.Printf("[scan] deleted_at update (in scan, hash reset) for scan %d took %d ms", scanID, time.Since(t3).Milliseconds())

	d := time.Since(deletedAtStart).Milliseconds()
	_ = db.UpdateScanDeletedAtUpdateDuration(ctx, q, scanID, d)
	log.Printf("[scan] deleted_at update for scan %d took %d ms total", scanID, d)
	return nil
}
