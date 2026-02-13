package scan

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/eargollo/ditto/internal/db"
)

// ScanOptions configures a scan run.
type ScanOptions struct {
	ExcludePatterns   []string
	MaxFilesPerSecond int
}

// RunScan walks rootPath, ensures a folder exists for it, creates a scan, upserts files and ledger rows, then sets the scan's completed_at.
// Uses the parallel pipeline (multiple walkers, batched DB writers). rootPath must be an existing directory. Returns scanID or error.
func RunScan(ctx context.Context, database *sql.DB, rootPath string, opts *ScanOptions) (int64, error) {
	rootPath = filepath.Clean(rootPath)
	info, err := os.Stat(rootPath)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, errors.New("root path is not a directory")
	}

	folderID, err := db.GetOrCreateFolderByPath(ctx, database, rootPath)
	if err != nil {
		return 0, err
	}
	folder, err := db.GetFolder(ctx, database, folderID)
	if err != nil {
		return 0, err
	}
	folderPath := folder.Path

	s, err := db.CreateScan(ctx, database, folderID)
	if err != nil {
		return 0, err
	}
	scanID := s.ID

	log.Printf("[scan] started for scan %d path %s (pipeline)", scanID, rootPath)
	fileCount, skippedScan, _, err := RunPipeline(ctx, database, scanID, folderID, rootPath, folderPath, opts, nil)
	if err != nil {
		return 0, err
	}
	if err := db.UpdateScanCompletedAt(ctx, database, scanID, fileCount, skippedScan); err != nil {
		return 0, err
	}
	return scanID, nil
}

// RunScanForExisting walks rootPath and upserts files + ledger for the existing scan (scanID). Use when the scan row was already created.
// Uses the parallel pipeline (multiple walkers, batched DB writers).
func RunScanForExisting(ctx context.Context, database *sql.DB, scanID int64, folderID int64, rootPath string, opts *ScanOptions) error {
	rootPath = filepath.Clean(rootPath)
	info, err := os.Stat(rootPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("root path is not a directory")
	}
	folder, err := db.GetFolder(ctx, database, folderID)
	if err != nil {
		return err
	}
	folderPath := folder.Path

	fileCount, skippedScan, _, err := RunPipeline(ctx, database, scanID, folderID, rootPath, folderPath, opts, nil)
	if err != nil {
		return err
	}
	return db.UpdateScanCompletedAt(ctx, database, scanID, fileCount, skippedScan)
}
