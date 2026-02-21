-- EXPLAIN (ANALYZE, BUFFERS) for the two "in scan" UPDATEs (undelete and hash reset).
-- Run in a single session with no other writers to avoid deadlock.
--
-- Usage (replace SCAN_ID with the real scan id, e.g. 3):
--   psql "$DATABASE_URL" -v scan_id=SCAN_ID -f internal/db/explain_deleted_at_in_scan_only.sql
--
-- The UPDATEs run inside BEGIN/ROLLBACK so no data is committed.

\echo '=== 1. EXPLAIN for "in scan, undelete" (then ROLLBACK) ==='
BEGIN;
EXPLAIN (ANALYZE, BUFFERS)
UPDATE files SET deleted_at = NULL
FROM file_scan WHERE files.id = file_scan.file_id AND file_scan.scan_id = :scan_id AND files.deleted_at IS NOT NULL;
ROLLBACK;

\echo ''
\echo '=== 2. EXPLAIN for "in scan, hash reset" (then ROLLBACK) ==='
BEGIN;
EXPLAIN (ANALYZE, BUFFERS)
UPDATE files SET hash_status = 'pending', hash = NULL, hashed_at = NULL, hashed_mtime = NULL
FROM file_scan WHERE files.id = file_scan.file_id AND file_scan.scan_id = :scan_id
AND (files.mtime IS DISTINCT FROM files.hashed_mtime OR files.hashed_mtime IS NULL);
ROLLBACK;

\echo ''
\echo '=== Row count: files in this scan that need hash reset (mtime != hashed_mtime or never hashed) ==='
SELECT count(*) AS rows_needing_hash_reset
FROM files f
JOIN file_scan fs ON f.id = fs.file_id AND fs.scan_id = :scan_id
WHERE f.mtime IS DISTINCT FROM f.hashed_mtime OR f.hashed_mtime IS NULL;

\echo ''
\echo '=== Row count: total files in this scan ==='
SELECT count(*) AS files_in_scan FROM file_scan WHERE scan_id = :scan_id;
