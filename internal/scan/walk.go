package scan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Entry holds metadata for a single regular file (no content).
type Entry struct {
	Path     string
	Size     int64
	MTime    int64
	Inode    int64
	DeviceID int64
}

// Walk traverses root and calls fn for each regular file. Symlinks are not
// followed and are not yielded (ADR-006). Directories are not yielded.
// Uses Lstat so symlink targets are never followed.
// If excludePatterns is non-nil and non-empty, paths matching any pattern are skipped
// (see ShouldExclude). Excluded directories are not recursed into.
func Walk(ctx context.Context, root string, excludePatterns []string, fn func(Entry) error) error {
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
		inode, dev := inodeAndDev(info)
		e := Entry{
			Path:     path,
			Size:     info.Size(),
			MTime:    info.ModTime().Unix(),
			Inode:    inode,
			DeviceID: dev,
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
