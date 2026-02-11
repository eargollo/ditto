package scan

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/eargollo/ditto/internal/db"
)

const scanProgressLogInterval = 1000

// ScanOptions configures a scan run.
type ScanOptions struct {
	ExcludePatterns   []string
	MaxFilesPerSecond int
}

// RunScan walks rootPath, ensures a folder exists for it, creates a scan, upserts files and ledger rows, then sets the scan's completed_at.
// rootPath must be an existing directory. Returns scanID or error.
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

	patterns := DefaultExcludePatterns()
	if opts != nil && len(opts.ExcludePatterns) > 0 {
		patterns = opts.ExcludePatterns
	}
	var maxFilesPerSecond int
	if opts != nil {
		maxFilesPerSecond = opts.MaxFilesPerSecond
	}

	var fileCount atomic.Int64
	var stats ScanStats
	var skippedScan int64
	stats.SkippedScan = &skippedScan
	err = Walk(ctx, rootPath, patterns, maxFilesPerSecond, &stats, func(e Entry) error {
		relPath, err := filepath.Rel(folderPath, e.Path)
		if err != nil {
			relPath = e.Path
		}
		fileID, err := db.UpsertFile(ctx, database, folderID, relPath, e.Size, e.MTime, e.Inode, e.DeviceID)
		if err != nil {
			return err
		}
		if err := db.InsertFileScan(ctx, database, fileID, scanID); err != nil {
			return err
		}
		n := fileCount.Add(1)
		if n%scanProgressLogInterval == 0 {
			log.Printf("[scan] %d files discovered (current: %s)", n, e.Path)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	if err := db.UpdateScanCompletedAt(ctx, database, scanID, fileCount.Load(), skippedScan); err != nil {
		return 0, err
	}
	return scanID, nil
}

// RunScanForExisting walks rootPath and upserts files + ledger for the existing scan (scanID). Use when the scan row was already created.
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

	patterns := DefaultExcludePatterns()
	if opts != nil && len(opts.ExcludePatterns) > 0 {
		patterns = opts.ExcludePatterns
	}
	var maxFilesPerSecond int
	if opts != nil {
		maxFilesPerSecond = opts.MaxFilesPerSecond
	}
	var fileCount atomic.Int64
	var stats ScanStats
	var skippedScan int64
	stats.SkippedScan = &skippedScan
	err = Walk(ctx, rootPath, patterns, maxFilesPerSecond, &stats, func(e Entry) error {
		relPath, err := filepath.Rel(folderPath, e.Path)
		if err != nil {
			relPath = e.Path
		}
		fileID, err := db.UpsertFile(ctx, database, folderID, relPath, e.Size, e.MTime, e.Inode, e.DeviceID)
		if err != nil {
			return err
		}
		if err := db.InsertFileScan(ctx, database, fileID, scanID); err != nil {
			return err
		}
		n := fileCount.Add(1)
		if n%scanProgressLogInterval == 0 {
			log.Printf("[scan] %d files discovered (current: %s)", n, e.Path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return db.UpdateScanCompletedAt(ctx, database, scanID, fileCount.Load(), skippedScan)
}
