package scan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/time/rate"
)

// Entry holds metadata for a single regular file (no content).
// DeviceID is nil when the OS does not provide a device id (e.g. Windows).
type Entry struct {
	Path     string
	Size     int64
	MTime    int64
	Inode    int64
	DeviceID *int64
}

// Walk traverses root and calls fn for each regular file. Symlinks are not
// followed and are not yielded (ADR-006). Directories are not yielded.
// Uses Lstat so symlink targets are never followed.
// If excludePatterns is non-nil and non-empty, paths matching any pattern are skipped
// (see ShouldExclude). Excluded directories are not recursed into.
// If maxFilesPerSecond > 0, fn is rate-limited to that many files per second (burst 1);
// if 0, no throttle (full speed).
func Walk(ctx context.Context, root string, excludePatterns []string, maxFilesPerSecond int, fn func(Entry) error) error {
	var limiter *rate.Limiter
	if maxFilesPerSecond > 0 {
		limiter = rate.NewLimiter(rate.Limit(maxFilesPerSecond), 1)
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if ShouldExclude(path, excludePatterns) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
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
			return err
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
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
				return err
			}
		}
		return fn(e)
	})
}

func inodeAndDev(info os.FileInfo) (inode, dev int64) {
	sys := info.Sys()
	if sys == nil {
		return 0, 0
	}
	if st, ok := sys.(*syscall.Stat_t); ok {
		return int64(st.Ino), int64(st.Dev)
	}
	return 0, 0
}
