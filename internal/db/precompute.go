package db

import (
	"context"
	"fmt"
)

// DuplicateGroupsHashSummary is the aggregate summary from duplicate_groups_hash (for landing page).
type DuplicateGroupsHashSummary struct {
	GroupCount     int64
	TotalFiles     int64
	TotalSize      int64
	ReclaimableSize int64
}

// PrecomputeDuplicateGroupsHash truncates duplicate_groups_hash and fills it from files where hash_status = 'done' and deleted_at IS NULL.
// Call after hash phase completes. No join to file_scan.
func PrecomputeDuplicateGroupsHash(ctx context.Context, q Querier) error {
	if _, err := q.ExecContext(ctx, "TRUNCATE duplicate_groups_hash"); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx,
		`INSERT INTO duplicate_groups_hash (hash, file_count, total_size, reclaimable_size)
		SELECT f.hash, COUNT(*)::bigint, COALESCE(SUM(f.size), 0),
		       COALESCE(SUM(f.size), 0) - (COALESCE(SUM(f.size), 0) / NULLIF(COUNT(*), 0))
		FROM files f
		WHERE f.hash_status = 'done' AND f.deleted_at IS NULL AND f.hash IS NOT NULL
		GROUP BY f.hash HAVING COUNT(*) > 1`)
	return err
}

// GetDuplicateGroupsHashSummary returns the summary (group count, total files, total size, reclaimable) from duplicate_groups_hash.
func GetDuplicateGroupsHashSummary(ctx context.Context, q Querier) (DuplicateGroupsHashSummary, error) {
	var s DuplicateGroupsHashSummary
	err := q.QueryRowContext(ctx,
		`SELECT COALESCE(COUNT(*), 0), COALESCE(SUM(file_count), 0), COALESCE(SUM(total_size), 0), COALESCE(SUM(reclaimable_size), 0)
		 FROM duplicate_groups_hash`).Scan(&s.GroupCount, &s.TotalFiles, &s.TotalSize, &s.ReclaimableSize)
	return s, err
}

// DuplicateGroupsHashPaginated returns a page of rows from duplicate_groups_hash ordered by total_size DESC.
func DuplicateGroupsHashPaginated(ctx context.Context, q Querier, limit, offset int) ([]DuplicateGroupByHash, error) {
	queryStr := `SELECT hash, file_count, total_size, reclaimable_size FROM duplicate_groups_hash ORDER BY total_size DESC`
	args := []interface{}{}
	if limit > 0 {
		// #nosec G202 -- placeholder index from len(args)+1; values passed as args
		queryStr += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, limit)
	}
	if offset > 0 {
		// #nosec G202 -- placeholder index from len(args)+1; values passed as args
		queryStr += fmt.Sprintf(" OFFSET $%d", len(args)+1)
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
		var reclaimable int64
		if err := rows.Scan(&g.Hash, &g.Count, &g.Size, &reclaimable); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// DuplicateGroupsHashCount returns the number of rows in duplicate_groups_hash.
func DuplicateGroupsHashCount(ctx context.Context, q Querier) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM duplicate_groups_hash").Scan(&n)
	return n, err
}
