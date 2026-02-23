package db

import (
	"context"
	"database/sql"
	"fmt"
)

// DuplicateGroupByHash is a group of files with the same content hash (duplicates).
type DuplicateGroupByHash struct {
	Hash  string
	Count int64
	Size  int64
}

// DuplicateGroupByInode is a group of files sharing the same inode (hardlinks).
type DuplicateGroupByInode struct {
	Inode    int64
	DeviceID *int64
	Count    int64
	Size     int64
}

// DuplicateGroupsByHash returns groups of files with the same hash (content duplicates) for the scan.
func DuplicateGroupsByHash(ctx context.Context, q Querier, scanID int64) ([]DuplicateGroupByHash, error) {
	return duplicateGroupsByHash(ctx, q, scanID, 0, 0)
}

// DuplicateGroupsByHashCount returns the number of duplicate-by-hash groups for the scan.
func DuplicateGroupsByHashCount(ctx context.Context, q Querier, scanID int64) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM (
			SELECT 1 FROM files f JOIN file_scan fs ON f.id = fs.file_id
			WHERE fs.scan_id = $1 AND f.hash_status = 'done'
			GROUP BY f.hash HAVING COUNT(*) > 1
		) sub`,
		scanID).Scan(&n)
	return n, err
}

// DuplicateGroupsByHashPaginated returns duplicate-by-hash groups for the scan with limit and offset.
func DuplicateGroupsByHashPaginated(ctx context.Context, q Querier, scanID int64, limit, offset int) ([]DuplicateGroupByHash, error) {
	return duplicateGroupsByHash(ctx, q, scanID, limit, offset)
}

func duplicateGroupsByHash(ctx context.Context, q Querier, scanID int64, limit, offset int) ([]DuplicateGroupByHash, error) {
	queryStr := `SELECT f.hash, COUNT(*), COALESCE(SUM(f.size), 0) FROM files f
		  JOIN file_scan fs ON f.id = fs.file_id
		  WHERE fs.scan_id = $1 AND f.hash_status = 'done'
		  GROUP BY f.hash HAVING COUNT(*) > 1
		  ORDER BY SUM(f.size) DESC`
	args := []interface{}{scanID}
	if limit > 0 {
		queryStr += fmt.Sprintf(" LIMIT $%d", len(args)+1) // #nosec G202 -- placeholder index only, args passed separately
		args = append(args, limit)
	}
	if offset > 0 {
		queryStr += fmt.Sprintf(" OFFSET $%d", len(args)+1) // #nosec G202 -- placeholder index only, args passed separately
		args = append(args, offset)
	}
	rows, err := q.QueryContext(ctx, queryStr, args...)
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

// DuplicateGroupsByHashCountAcrossScans returns the number of duplicate-by-hash groups across the given scans.
func DuplicateGroupsByHashCountAcrossScans(ctx context.Context, q Querier, scanIDs []int64) (int64, error) {
	if len(scanIDs) == 0 {
		return 0, nil
	}
	ph := placeholders(len(scanIDs), 1)
	queryStr := `SELECT COUNT(*) FROM (
		SELECT 1 FROM files f JOIN file_scan fs ON f.id = fs.file_id
		WHERE fs.scan_id IN (` + ph + `) AND f.hash_status = 'done'
		GROUP BY f.hash HAVING COUNT(*) > 1
	) sub`
	args := idSlice(scanIDs)
	var n int64
	err := q.QueryRowContext(ctx, queryStr, args...).Scan(&n)
	return n, err
}

// DuplicateGroupsByHashPaginatedAcrossScans returns duplicate-by-hash groups across the given scans.
func DuplicateGroupsByHashPaginatedAcrossScans(ctx context.Context, q Querier, scanIDs []int64, limit, offset int) ([]DuplicateGroupByHash, error) {
	if len(scanIDs) == 0 {
		return nil, nil
	}
	ph := placeholders(len(scanIDs), 1)
	queryStr := `SELECT f.hash, COUNT(*), COALESCE(SUM(f.size), 0) FROM files f
		  JOIN file_scan fs ON f.id = fs.file_id
		  WHERE fs.scan_id IN (` + ph + `) AND f.hash_status = 'done'
		  GROUP BY f.hash HAVING COUNT(*) > 1
		  ORDER BY SUM(f.size) DESC` // #nosec G202 -- ph is placeholder count; args passed separately
	args := idSlice(scanIDs)
	if limit > 0 {
		queryStr += fmt.Sprintf(" LIMIT $%d", len(args)+1) // #nosec G202 -- placeholder index only
		args = append(args, limit)
	}
	if offset > 0 {
		queryStr += fmt.Sprintf(" OFFSET $%d", len(args)+1) // #nosec G202 -- placeholder index only
		args = append(args, offset)
	}
	rows, err := q.QueryContext(ctx, queryStr, args...)
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

// FilesInHashGroupLimitAcrossScans returns up to limit files with the given hash in any of the given scans.
func FilesInHashGroupLimitAcrossScans(ctx context.Context, q Querier, scanIDs []int64, hash string, limit int) ([]File, error) {
	if len(scanIDs) == 0 {
		return nil, nil
	}
	ph := placeholders(len(scanIDs), 1)
	queryStr := `SELECT f.id, fs.scan_id, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at
		  FROM files f JOIN file_scan fs ON f.id = fs.file_id JOIN folders fo ON f.folder_id = fo.id
		  WHERE fs.scan_id IN (` + ph + `) AND f.hash_status = 'done' AND f.hash = $` + fmt.Sprint(len(scanIDs)+1) + ` ORDER BY fs.scan_id, f.path` // #nosec G202 -- ph and placeholder index; args passed separately
	args := idSlice(scanIDs)
	args = append(args, hash)
	if limit > 0 {
		queryStr += fmt.Sprintf(" LIMIT $%d", len(args)+1) // #nosec G202 -- placeholder index only
		args = append(args, limit)
	}
	rows, err := q.QueryContext(ctx, queryStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFiles(rows)
}

func placeholders(n, start int) string {
	if n <= 0 {
		return ""
	}
	s := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("$%d", start+i)
	}
	return s
}

func idSlice(ids []int64) []interface{} {
	out := make([]interface{}, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// FilesInHashGroupAcrossScans returns all files with the given hash in any of the given scans.
func FilesInHashGroupAcrossScans(ctx context.Context, q Querier, scanIDs []int64, hash string) ([]File, error) {
	return FilesInHashGroupLimitAcrossScans(ctx, q, scanIDs, hash, 0)
}

// DuplicateGroupsByInode returns groups of files with the same (inode, device_id) (hardlinks) for the scan.
func DuplicateGroupsByInode(ctx context.Context, q Querier, scanID int64) ([]DuplicateGroupByInode, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT f.inode, f.device_id, COUNT(*), COALESCE(SUM(f.size), 0) FROM files f
		 JOIN file_scan fs ON f.id = fs.file_id
		 WHERE fs.scan_id = $1 AND f.inode IS NOT NULL
		 GROUP BY f.inode, COALESCE(f.device_id, -999)
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
func FilesInHashGroup(ctx context.Context, q Querier, scanID int64, hash string) ([]File, error) {
	return filesInHashGroup(ctx, q, scanID, hash, 0)
}

// FileForGroupRefresh is a file row used when refreshing a duplicate group (check existence and mtime).
type FileForGroupRefresh struct {
	ID          int64
	Path        string  // full path (folder path + / + file path)
	Size        int64
	HashedMtime *int64  // mtime at hash time; nil if never stored
}

// FilesForGroupRefresh returns all files with the given hash and deleted_at IS NULL, with path/size/hashed_mtime for refresh.
func FilesForGroupRefresh(ctx context.Context, q Querier, hash string) ([]FileForGroupRefresh, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT f.id, (fo.path || '/' || f.path), f.size, f.hashed_mtime
		  FROM files f JOIN folders fo ON f.folder_id = fo.id
		  WHERE f.hash = $1 AND f.hash_status = 'done' AND f.deleted_at IS NULL ORDER BY f.path`,
		hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileForGroupRefresh
	for rows.Next() {
		var r FileForGroupRefresh
		var hashedMtime sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Path, &r.Size, &hashedMtime); err != nil {
			return nil, err
		}
		if hashedMtime.Valid {
			r.HashedMtime = &hashedMtime.Int64
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FilesInHashGroupByHash returns all files (across folders) with the given hash and deleted_at IS NULL. No file_scan join.
// limit 0 means no limit.
func FilesInHashGroupByHash(ctx context.Context, q Querier, hash string, limit int) ([]File, error) {
	queryStr := `SELECT f.id, 0::bigint, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at
		  FROM files f JOIN folders fo ON f.folder_id = fo.id
		  WHERE f.hash = $1 AND f.hash_status = 'done' AND f.deleted_at IS NULL ORDER BY f.path`
	args := []interface{}{hash}
	if limit > 0 {
		queryStr += fmt.Sprintf(" LIMIT $%d", len(args)+1) // #nosec G202
		args = append(args, limit)
	}
	rows, err := q.QueryContext(ctx, queryStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFiles(rows)
}

// FilesInHashGroupLimit returns up to limit files in the scan with the given hash.
func FilesInHashGroupLimit(ctx context.Context, q Querier, scanID int64, hash string, limit int) ([]File, error) {
	return filesInHashGroup(ctx, q, scanID, hash, limit)
}

func filesInHashGroup(ctx context.Context, q Querier, scanID int64, hash string, limit int) ([]File, error) {
	queryStr := `SELECT f.id, fs.scan_id, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at
		  FROM files f JOIN file_scan fs ON f.id = fs.file_id JOIN folders fo ON f.folder_id = fo.id
		  WHERE fs.scan_id = $1 AND f.hash_status = 'done' AND f.hash = $2 ORDER BY f.path`
	args := []interface{}{scanID, hash}
	if limit > 0 {
		queryStr += fmt.Sprintf(" LIMIT $%d", len(args)+1) // #nosec G202 -- placeholder index only
		args = append(args, limit)
	}
	rows, err := q.QueryContext(ctx, queryStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFiles(rows)
}

// FilesInInodeGroup returns all files in the scan with the given inode (and device_id if not nil).
func FilesInInodeGroup(ctx context.Context, q Querier, scanID int64, inode int64, deviceID *int64) ([]File, error) {
	var queryStr string
	var args []interface{}
	if deviceID != nil {
		queryStr = `SELECT f.id, fs.scan_id, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at
			 FROM files f JOIN file_scan fs ON f.id = fs.file_id JOIN folders fo ON f.folder_id = fo.id
			 WHERE fs.scan_id = $1 AND f.inode = $2 AND f.device_id = $3 ORDER BY f.path`
		args = []interface{}{scanID, inode, *deviceID}
	} else {
		queryStr = `SELECT f.id, fs.scan_id, (fo.path || '/' || f.path), f.size, f.mtime, f.inode, f.device_id, f.hash, f.hash_status, f.hashed_at
			 FROM files f JOIN file_scan fs ON f.id = fs.file_id JOIN folders fo ON f.folder_id = fo.id
			 WHERE fs.scan_id = $1 AND f.inode = $2 AND f.device_id IS NULL ORDER BY f.path`
		args = []interface{}{scanID, inode}
	}
	rows, err := q.QueryContext(ctx, queryStr, args...)
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
		var inode sql.NullInt64
		var deviceID sql.NullInt64
		var hash sql.NullString
		var hashedAt nullRFC3339Time
		if err := rows.Scan(&f.ID, &f.ScanID, &f.Path, &f.Size, &f.MTime, &inode, &deviceID, &hash, &f.HashStatus, &hashedAt); err != nil {
			return nil, err
		}
		if inode.Valid {
			v := inode.Int64
			f.Inode = &v
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
