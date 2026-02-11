package scan

import (
	"context"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/time/rate"
)

// DebugScanEnv is the name of the env var that enables directory logging to find hang locations.
// When set (e.g. DITTO_DEBUG_SCAN=1), we log each directory we're about to list; if the scan hangs,
// the last "[scan] listing directory: <path>" line is the path to add to default.dittoignore.
const DebugScanEnv = "DITTO_DEBUG_SCAN"

// Entry holds metadata for a single regular file (no content).
// DeviceID is nil when the OS does not provide a device id (e.g. Windows).
type Entry struct {
	Path     string
	Size     int64
	MTime    int64
	Inode    int64
	DeviceID *int64
}

// ScanStats holds optional counters updated during Walk (e.g. paths skipped).
// Pass to Walk to collect stats; nil fields are not updated.
type ScanStats struct {
	SkippedScan *int64 // paths skipped at scan (permission or exclude)
}

// Walk traverses root and calls fn for each regular file. Symlinks are not
// followed and are not yielded (ADR-006). Directories are not yielded.
// Uses Lstat so symlink targets are never followed.
// If excludePatterns is non-nil and non-empty, paths matching any pattern are skipped
// (see ShouldExclude). Excluded directories are not recursed into.
// If stats is non-nil, SkippedScan is incremented for each path skipped (permission or exclude).
// If maxFilesPerSecond > 0, fn is rate-limited to that many files per second (burst 1);
// if 0, no throttle (full speed).
func Walk(ctx context.Context, root string, excludePatterns []string, maxFilesPerSecond int, stats *ScanStats, fn func(Entry) error) error {
	var limiter *rate.Limiter
	if maxFilesPerSecond > 0 {
		limiter = rate.NewLimiter(rate.Limit(maxFilesPerSecond), 1)
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if isPermissionOrAccessError(err) {
				if stats != nil && stats.SkippedScan != nil {
					*stats.SkippedScan++
				}
				log.Printf("[scan] skipped (permission): %s: %v", path, err)
				return filepath.SkipDir
			}
			log.Printf("[scan] error at %s: %v", path, err)
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if ShouldExclude(path, excludePatterns) {
			if stats != nil && stats.SkippedScan != nil {
				*stats.SkippedScan++
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if os.Getenv(DebugScanEnv) != "" {
				log.Printf("[scan] listing directory: %s", path)
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			log.Printf("[scan] error at %s (Lstat): %v", path, err)
			return err
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			log.Printf("[scan] error at %s (Abs): %v", path, err)
			return err
		}
		inode, dev := inodeAndDev(info)
		var deviceID *int64
		if dev != 0 {
			deviceID = &dev
		}
		e := Entry{
			Path:     absPath,
			Size:     info.Size(),
			MTime:    info.ModTime().Unix(),
			Inode:    inode,
			DeviceID: deviceID,
		}
		if limiter != nil {
			if err := limiter.Wait(ctx); err != nil {
				log.Printf("[scan] error at %s (rate limit): %v", path, err)
				return err
			}
		}
		if err := fn(e); err != nil {
			log.Printf("[scan] error at %s: %v", absPath, err)
			return err
		}
		return nil
	})
}

func isPermissionOrAccessError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsPermission(err) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "operation not permitted") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "Permission denied")
}

func inodeAndDev(info os.FileInfo) (inode, dev int64) {
	sys := info.Sys()
	if sys == nil {
		return 0, 0
	}
	if st, ok := sys.(*syscall.Stat_t); ok {
		return statTToInt64(any(st.Ino)), statTToInt64(any(st.Dev))
	}
	return 0, 0
}

// statTToInt64 converts Stat_t Ino/Dev to int64 without overflow (type varies by OS: uint64, int32, etc.).
func statTToInt64(v interface{}) int64 {
	switch x := v.(type) {
	case uint64:
		if x > math.MaxInt64 {
			return 0
		}
		return int64(x)
	case int32:
		return int64(x)
	case uint32:
		if x > math.MaxInt32 {
			return 0
		}
		return int64(x)
	default:
		return 0
	}
}
