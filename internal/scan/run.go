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

const scanProgressLogInterval = 1000 // log "Scanned N files" every this many files

// ScanOptions configures a scan run (excludes and optional throttle).
// MaxFilesPerSecond limits how many files are yielded per second during the walk;
// 0 means no throttle (full speed).
type ScanOptions struct {
	ExcludePatterns   []string
	MaxFilesPerSecond int
}

// RunScan walks rootPath, inserts file rows for each regular file (respecting excludes),
// then sets the scan's completed_at. If rootPath does not exist or is not a directory,
// returns an error without creating a scan row. On walk or insert failure, returns the
// error and leaves the scan row without completed_at (incomplete scan).
func RunScan(ctx context.Context, database *sql.DB, rootPath string, opts *ScanOptions) (int64, error) {
	rootPath = filepath.Clean(rootPath)
	info, err := os.Stat(rootPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		return 0, err
	}
	if !info.IsDir() {
		return 0, errors.New("root path is not a directory")
	}

	patterns := DefaultExcludePatterns()
	if opts != nil && len(opts.ExcludePatterns) > 0 {
		patterns = opts.ExcludePatterns
	}
	var maxFilesPerSecond int
	if opts != nil {
		maxFilesPerSecond = opts.MaxFilesPerSecond
	}

	s, err := db.CreateScan(ctx, database, rootPath)
	if err != nil {
		return 0, err
	}
	scanID := s.ID

	var fileCount atomic.Int64
	var stats ScanStats
	var skippedScan int64
	stats.SkippedScan = &skippedScan
	err = Walk(ctx, rootPath, patterns, maxFilesPerSecond, &stats, func(e Entry) error {
		if err := db.InsertFile(ctx, database, scanID, e.Path, e.Size, e.MTime, e.Inode, e.DeviceID); err != nil {
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

// RunScanForExisting walks rootPath and inserts file rows for the existing scan (scanID).
// Use when the scan row was already created (e.g. by CreateScan). Sets completed_at when done.
// Returns an error if rootPath does not exist or is not a directory, or on walk/insert failure.
func RunScanForExisting(ctx context.Context, database *sql.DB, scanID int64, rootPath string, opts *ScanOptions) error {
	rootPath = filepath.Clean(rootPath)
	info, err := os.Stat(rootPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("root path is not a directory")
	}
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
		if err := db.InsertFile(ctx, database, scanID, e.Path, e.Size, e.MTime, e.Inode, e.DeviceID); err != nil {
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
