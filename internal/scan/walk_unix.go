//go:build unix

package scan

import (
	"math"
	"os"
	"syscall"
)

func inodeAndDev(info os.FileInfo) (inode, dev int64) {
	sys := info.Sys()
	if sys == nil {
		return InvalidInode, InvalidInode
	}
	if st, ok := sys.(*syscall.Stat_t); ok {
		inode := statTToInt64(any(st.Ino))
		dev := statTToInt64(any(st.Dev))
		if inode == 0 && dev == 0 {
			return InvalidInode, InvalidInode
		}
		return inode, dev
	}
	return InvalidInode, InvalidInode
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
