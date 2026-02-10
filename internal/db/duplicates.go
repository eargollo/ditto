package db

import (
	"context"
	"database/sql"
)

// DuplicateGroupByHash is a group of files with the same content hash (duplicates).
type DuplicateGroupByHash struct {
	Hash  string
	Count int64
	Size  int64 // sum of file sizes in the group
}

// DuplicateGroupByInode is a group of files sharing the same inode (hardlinks).
type DuplicateGroupByInode struct {
	Inode    int64
	DeviceID *int64
	Count    int64
	Size     int64
}

// DuplicateGroupsByHash returns groups of files with the same hash (content duplicates) for the scan.
func DuplicateGroupsByHash(ctx context.Context, database *sql.DB, scanID int64) ([]DuplicateGroupByHash, error) {
	return duplicateGroupsByHash(ctx, database, scanID, 0, 0)
}

// DuplicateGroupsByHashCount returns the number of duplicate-by-hash groups for the scan (for pagination).
// Only counts files with hash_status = 'done' so we don't touch hot rows (pending/hashing) during a scan.
func DuplicateGroupsByHashCount(ctx context.Context, database *sql.DB, scanID int64) (int64, error) {
	var n int64
	err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM (
			SELECT 1 FROM files
			WHERE scan_id = ? AND hash_status = 'done'
			GROUP BY hash HAVING COUNT(*) > 1
		)`,
		scanID).Scan(&n)
	return n, err
}

// DuplicateGroupsByHashPaginated returns duplicate-by-hash groups ordered by group size (sum of file sizes) descending, with limit and offset.
func DuplicateGroupsByHashPaginated(ctx context.Context, database *sql.DB, scanID int64, limit, offset int) ([]DuplicateGroupByHash, error) {
	return duplicateGroupsByHash(ctx, database, scanID, limit, offset)
}

func duplicateGroupsByHash(ctx context.Context, database *sql.DB, scanID int64, limit, offset int) ([]DuplicateGroupByHash, error) {
	q := `SELECT hash, COUNT(*), COALESCE(SUM(size), 0) FROM files
		  WHERE scan_id = ? AND hash_status = 'done'
		  GROUP BY hash HAVING COUNT(*) > 1
		  ORDER BY SUM(size) DESC`
	args := []interface{}{scanID}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		q += " OFFSET ?"
		args = append(args, offset)
	}
	rows, err := database.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []DuplicateGroupByHash
	for rows.Next() {
		var g DuplicateGroupByHash
		if err := rows.Scan(&g.Hash, &g.Count, &g.Size); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// DuplicateGroupsByHashCountAcrossScans returns the number of duplicate-by-hash groups when considering only files in the given scans (e.g. latest scan per folder). Empty scanIDs returns 0.
func DuplicateGroupsByHashCountAcrossScans(ctx context.Context, database *sql.DB, scanIDs []int64) (int64, error) {
	if len(scanIDs) == 0 {
		return 0, nil
	}
	placeholders := placeholdersForIDs(scanIDs)
	q := `SELECT COUNT(*) FROM (
		SELECT 1 FROM files
		WHERE scan_id IN (` + placeholders + `) AND hash_status = 'done'
		GROUP BY hash HAVING COUNT(*) > 1
	)`
	args := idSlice(scanIDs)
	var n int64
	err := database.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// DuplicateGroupsByHashPaginatedAcrossScans returns duplicate-by-hash groups across the given scans, ordered by group size descending. Empty scanIDs returns nil.
func DuplicateGroupsByHashPaginatedAcrossScans(ctx context.Context, database *sql.DB, scanIDs []int64, limit, offset int) ([]DuplicateGroupByHash, error) {
	if len(scanIDs) == 0 {
		return nil, nil
	}
	placeholders := placeholdersForIDs(scanIDs)
	q := `SELECT hash, COUNT(*), COALESCE(SUM(size), 0) FROM files
		  WHERE scan_id IN (` + placeholders + `) AND hash_status = 'done'
		  GROUP BY hash HAVING COUNT(*) > 1
		  ORDER BY SUM(size) DESC`
	args := idSlice(scanIDs)
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		q += " OFFSET ?"
		args = append(args, offset)
	}
	rows, err := database.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []DuplicateGroupByHash
	for rows.Next() {
		var g DuplicateGroupByHash
		if err := rows.Scan(&g.Hash, &g.Count, &g.Size); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// FilesInHashGroupLimitAcrossScans returns up to limit files with the given hash in any of the given scans. Empty scanIDs returns nil.
func FilesInHashGroupLimitAcrossScans(ctx context.Context, database *sql.DB, scanIDs []int64, hash string, limit int) ([]File, error) {
	if len(scanIDs) == 0 {
		return nil, nil
	}
	placeholders := placeholdersForIDs(scanIDs)
	q := "SELECT id, scan_id, path, size, mtime, inode, device_id, hash, hash_status, hashed_at FROM files WHERE scan_id IN (" + placeholders + ") AND hash_status = 'done' AND hash = ? ORDER BY scan_id, path"
	args := make([]interface{}, 0, len(scanIDs)+2)
	for _, id := range scanIDs {
		args = append(args, id)
	}
	args = append(args, hash)
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := database.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFiles(rows)
}

// FilesInHashGroupAcrossScans returns all files with the given hash in any of the given scans. Empty scanIDs returns nil.
func FilesInHashGroupAcrossScans(ctx context.Context, database *sql.DB, scanIDs []int64, hash string) ([]File, error) {
	return FilesInHashGroupLimitAcrossScans(ctx, database, scanIDs, hash, 0)
}

func placeholdersForIDs(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	b := make([]byte, 0, len(ids)*3)
	for i := range ids {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}

func idSlice(ids []int64) []interface{} {
	out := make([]interface{}, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// DuplicateGroupsByInode returns groups of files with the same (inode, device_id) (hardlinks) for the scan.
func DuplicateGroupsByInode(ctx context.Context, database *sql.DB, scanID int64) ([]DuplicateGroupByInode, error) {
	rows, err := database.QueryContext(ctx,
		`SELECT inode, device_id, COUNT(*), COALESCE(SUM(size), 0) FROM files
		 WHERE scan_id = ?
		 GROUP BY inode, COALESCE(device_id, -999)
		 HAVING COUNT(*) > 1
		 ORDER BY COUNT(*) DESC`,
		scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []DuplicateGroupByInode
	for rows.Next() {
		var g DuplicateGroupByInode
		var deviceID sql.NullInt64
		if err := rows.Scan(&g.Inode, &deviceID, &g.Count, &g.Size); err != nil {
			return nil, err
		}
		if deviceID.Valid && deviceID.Int64 != -999 {
			v := deviceID.Int64
			g.DeviceID = &v
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// FilesInHashGroup returns all files in the scan that have the given hash.
func FilesInHashGroup(ctx context.Context, database *sql.DB, scanID int64, hash string) ([]File, error) {
	return filesInHashGroup(ctx, database, scanID, hash, 0)
}

// FilesInHashGroupLimit returns up to limit files in the scan with the given hash (for home page preview).
func FilesInHashGroupLimit(ctx context.Context, database *sql.DB, scanID int64, hash string, limit int) ([]File, error) {
	return filesInHashGroup(ctx, database, scanID, hash, limit)
}

func filesInHashGroup(ctx context.Context, database *sql.DB, scanID int64, hash string, limit int) ([]File, error) {
	q := "SELECT id, scan_id, path, size, mtime, inode, device_id, hash, hash_status, hashed_at FROM files WHERE scan_id = ? AND hash_status = 'done' AND hash = ? ORDER BY path"
	args := []interface{}{scanID, hash}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := database.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFiles(rows)
}

// FilesInInodeGroup returns all files in the scan with the given inode (and device_id if not nil).
func FilesInInodeGroup(ctx context.Context, database *sql.DB, scanID int64, inode int64, deviceID *int64) ([]File, error) {
	var rows *sql.Rows
	var err error
	if deviceID != nil {
		rows, err = database.QueryContext(ctx,
			"SELECT id, scan_id, path, size, mtime, inode, device_id, hash, hash_status, hashed_at FROM files WHERE scan_id = ? AND inode = ? AND device_id = ? ORDER BY path",
			scanID, inode, *deviceID)
	} else {
		rows, err = database.QueryContext(ctx,
			"SELECT id, scan_id, path, size, mtime, inode, device_id, hash, hash_status, hashed_at FROM files WHERE scan_id = ? AND inode = ? AND device_id IS NULL ORDER BY path",
			scanID, inode)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFiles(rows)
}

func scanFiles(rows *sql.Rows) ([]File, error) {
	var files []File
	for rows.Next() {
		var f File
		var deviceID sql.NullInt64
		var hash sql.NullString
		var hashedAt nullRFC3339Time
		if err := rows.Scan(&f.ID, &f.ScanID, &f.Path, &f.Size, &f.MTime, &f.Inode, &deviceID, &hash, &f.HashStatus, &hashedAt); err != nil {
			return nil, err
		}
		if deviceID.Valid {
			v := deviceID.Int64
			f.DeviceID = &v
		}
		if hash.Valid {
			s := hash.String
			f.Hash = &s
		}
		f.HashedAt = hashedAt.Ptr()
		files = append(files, f)
	}
	return files, rows.Err()
}
