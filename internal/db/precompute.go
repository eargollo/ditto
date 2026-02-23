package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const refreshDuplicateGroupsHashChunkSize = 500

// DuplicateGroupsHashSummary is the aggregate summary from duplicate_groups_hash (for landing page).
type DuplicateGroupsHashSummary struct {
	GroupCount     int64
	TotalFiles     int64
	TotalSize      int64
	ReclaimableSize int64
}

const precomputeDuplicateGroupsHashInsert = `INSERT INTO duplicate_groups_hash (hash, file_count, total_size, reclaimable_size)
		SELECT f.hash, COUNT(*)::bigint, COALESCE(SUM(f.size), 0),
		       COALESCE(SUM(f.size), 0) - (COALESCE(SUM(f.size), 0) / NULLIF(COUNT(*), 0))
		FROM files f
		WHERE f.hash_status = 'done' AND f.deleted_at IS NULL AND f.hash IS NOT NULL
		GROUP BY f.hash HAVING COUNT(*) > 1`

// PrecomputeDuplicateGroupsHash truncates duplicate_groups_hash and fills it from files where hash_status = 'done' and deleted_at IS NULL.
// Call after hash phase completes. No join to file_scan.
// When q is *sql.DB, prefer PrecomputeDuplicateGroupsHashInTransaction so the table is not visible empty between TRUNCATE and INSERT.
func PrecomputeDuplicateGroupsHash(ctx context.Context, q Querier) error {
	if _, err := q.ExecContext(ctx, "TRUNCATE duplicate_groups_hash"); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, precomputeDuplicateGroupsHashInsert)
	return err
}

// PrecomputeDuplicateGroupsHashInTransaction runs TRUNCATE + INSERT inside a single transaction so duplicate_groups_hash
// is never visible empty to readers: they see the previous state until the new state is committed.
// Call for admin "Refresh all groups" or to seed an empty table; normal flow uses RefreshDuplicateGroupsForHashes.
func PrecomputeDuplicateGroupsHashInTransaction(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "TRUNCATE duplicate_groups_hash"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, precomputeDuplicateGroupsHashInsert); err != nil {
		return err
	}
	return tx.Commit()
}

// RefreshDuplicateGroupsForHashes updates duplicate_groups_hash for the given hashes only: removes existing rows
// for those hashes, then re-inserts from files (hash_status = 'done', deleted_at IS NULL) for hashes that still
// have more than one file. Use after hash batch writes, after marking files deleted (not in scan), and after
// hash reset (in scan). Empty hashes is a no-op. Chunks the list to keep query size reasonable.
func RefreshDuplicateGroupsForHashes(ctx context.Context, q Querier, hashes []string) error {
	if len(hashes) == 0 {
		return nil
	}
	// Deduplicate so we don't process the same hash in multiple chunks
	seen := make(map[string]struct{}, len(hashes))
	var deduped []string
	for _, h := range hashes {
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		deduped = append(deduped, h)
	}
	if len(deduped) == 0 {
		return nil
	}
	for start := 0; start < len(deduped); start += refreshDuplicateGroupsHashChunkSize {
		end := start + refreshDuplicateGroupsHashChunkSize
		if end > len(deduped) {
			end = len(deduped)
		}
		chunk := deduped[start:end]
		if err := refreshDuplicateGroupsForHashesChunk(ctx, q, chunk); err != nil {
			return err
		}
	}
	return nil
}

func refreshDuplicateGroupsForHashesChunk(ctx context.Context, q Querier, hashes []string) error {
	if len(hashes) == 0 {
		return nil
	}
	placeholders := make([]string, len(hashes))
	args := make([]interface{}, 0, len(hashes))
	for i, h := range hashes {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, h)
	}
	inList := strings.Join(placeholders, ", ")
	// Remove existing rows for these hashes so we can re-insert from current files.
	// #nosec G202 -- placeholders from len(hashes); values in args
	deleteQ := "DELETE FROM duplicate_groups_hash WHERE hash IN (" + inList + ")"
	if _, err := q.ExecContext(ctx, deleteQ, args...); err != nil {
		return err
	}
	// Re-insert only hashes that still have >1 non-deleted done file (same formula as full precompute).
	// #nosec G202 -- placeholders from len(hashes); values in args
	insertQ := `INSERT INTO duplicate_groups_hash (hash, file_count, total_size, reclaimable_size)
		SELECT f.hash, COUNT(*)::bigint, COALESCE(SUM(f.size), 0),
		       COALESCE(SUM(f.size), 0) - (COALESCE(SUM(f.size), 0) / NULLIF(COUNT(*), 0))
		FROM files f
		WHERE f.hash_status = 'done' AND f.deleted_at IS NULL AND f.hash IS NOT NULL AND f.hash IN (` + inList + `)
		GROUP BY f.hash HAVING COUNT(*) > 1`
	_, err := q.ExecContext(ctx, insertQ, args...)
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
