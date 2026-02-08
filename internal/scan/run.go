package scan

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"github.com/eargollo/ditto/internal/db"
)

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

	var patterns []string
	var maxFilesPerSecond int
	if opts != nil {
		patterns = opts.ExcludePatterns
		maxFilesPerSecond = opts.MaxFilesPerSecond
	}

	s, err := db.CreateScan(ctx, database, rootPath)
	if err != nil {
		return 0, err
	}
	scanID := s.ID

	err = Walk(ctx, rootPath, patterns, maxFilesPerSecond, func(e Entry) error {
		return db.InsertFile(ctx, database, scanID, e.Path, e.Size, e.MTime, e.Inode, e.DeviceID)
	})
	if err != nil {
		return 0, err
	}

	if err := db.UpdateScanCompletedAt(ctx, database, scanID); err != nil {
		return 0, err
	}
	return scanID, nil
}
