//go:build windows

package scan

import "os"

func inodeAndDev(info os.FileInfo) (inode, dev int64) {
	return InvalidInode, InvalidInode
}
