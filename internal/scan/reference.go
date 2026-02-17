package scan

import (
	"bytes"
	"context"
	"encoding/csv"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/eargollo/ditto/internal/hash"
)

// ReferenceProgressFunc is called during ReferenceCSV to report progress. Phase is "walk" or "hash";
// current is the number of files so far; total is the total (or -1 if unknown, e.g. during walk).
type ReferenceProgressFunc func(phase string, current, total int64)

// ReferenceStats holds the same conceptual counts as the DB scan row, for comparing reference vs service.
type ReferenceStats struct {
	FileCount          int64   // files walked (in scan)
	ScanSkippedCount   int64   // paths skipped (permission/exclude)
	HashedFileCount    int64   // files we hashed (candidates only)
	HashedByteCount    int64   // sum of sizes of hashed files
	HashReusedCount    int64   // hashes reused via inode (hardlinks)
	HashErrorCount     int64   // hash failures
	WalkDurationSec    float64 // seconds to walk
	HashDurationSec    float64 // seconds to hash candidates
}

// referenceRow is used to build the reference CSV (path, hash, size).
type referenceRow struct {
	path string
	hash string
	size int64
}

// inodeKey is used to cache hash by (inode, device_id) for hardlink reuse (same as hash phase).
// deviceID -1 means nil (no device).
type inodeKey struct {
	inode   int64
	deviceID int64
}

func makeInodeKey(e Entry) inodeKey {
	k := inodeKey{inode: e.Inode, deviceID: -1}
	if e.DeviceID != nil {
		k.deviceID = *e.DeviceID
	}
	return k
}

// candidateSizes returns sizes that appear at least twice in entries (same as DB: only hash these).
func candidateSizes(entries []Entry) map[int64]bool {
	sizeCount := make(map[int64]int)
	for _, e := range entries {
		sizeCount[e.Size]++
	}
	out := make(map[int64]bool)
	for size, n := range sizeCount {
		if n >= 2 {
			out[size] = true
		}
	}
	return out
}

// ReferenceCSV runs a linear walk over root with the given exclude patterns,
// hashes only duplicate candidates (files whose size appears at least twice, same as DB),
// and returns CSV bytes (path, hash, size) sorted by path plus stats. Non-candidates get empty hash.
// If progress is non-nil, it is called during walk and hash phases (total is -1 during walk).
func ReferenceCSV(ctx context.Context, root string, excludePatterns []string, progress ReferenceProgressFunc) ([]byte, *ReferenceStats, error) {
	stats := &ReferenceStats{}
	walkStart := time.Now()
	var entries []Entry
	var walkCount int64
	scanStats := &ScanStats{SkippedScan: &stats.ScanSkippedCount}
	err := Walk(ctx, root, excludePatterns, 0, scanStats, func(e Entry) error {
		entries = append(entries, e)
		walkCount++
		if progress != nil && walkCount%1000 == 0 {
			progress("walk", walkCount, -1)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	stats.WalkDurationSec = time.Since(walkStart).Seconds()
	stats.FileCount = walkCount
	if progress != nil {
		progress("walk", walkCount, -1)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	candidates := candidateSizes(entries)
	var totalCandidates int64
	for _, e := range entries {
		if candidates[e.Size] {
			totalCandidates++
		}
	}
	hashStart := time.Now()
	cache := make(map[inodeKey]string)
	var rows []referenceRow
	var hashCount int64
	for _, e := range entries {
		if !candidates[e.Size] {
			rows = append(rows, referenceRow{path: e.Path, hash: "", size: e.Size})
			continue
		}
		var h string
		if e.Inode == InvalidInode {
			// Cannot reuse: inode unknown (e.g. Windows). Hash every time.
			var hashErr error
			h, hashErr = hash.HashFile(e.Path)
			if hashErr != nil {
				stats.HashErrorCount++
				return nil, nil, hashErr
			}
			stats.HashedFileCount++
			stats.HashedByteCount += e.Size
		} else {
			key := makeInodeKey(e)
			var ok bool
			h, ok = cache[key]
			if !ok {
				var hashErr error
				h, hashErr = hash.HashFile(e.Path)
				if hashErr != nil {
					stats.HashErrorCount++
					return nil, nil, hashErr
				}
				cache[key] = h
				stats.HashedFileCount++
				stats.HashedByteCount += e.Size
			} else {
				stats.HashReusedCount++
			}
		}
		rows = append(rows, referenceRow{path: e.Path, hash: h, size: e.Size})
		hashCount++
		if progress != nil && (hashCount%500 == 0 || hashCount == totalCandidates) {
			progress("hash", hashCount, totalCandidates)
		}
	}
	stats.HashDurationSec = time.Since(hashStart).Seconds()

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"path", "hash", "size"}); err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		// Normalize to forward slashes so CSV is comparable across OS (e.g. reference on Windows vs export on Linux).
		if err := w.Write([]string{filepath.ToSlash(r.path), r.hash, strconv.FormatInt(r.size, 10)}); err != nil {
			return nil, nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), stats, nil
}
