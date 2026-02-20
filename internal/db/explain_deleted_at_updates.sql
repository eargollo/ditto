-- Query analysis for deleted_at update (not in scan) and (in scan).
-- Run in psql against your DB. Sections 1 and 2 run the real UPDATE then ROLLBACK.
--
-- Usage:
--   psql "postgres://..." -v scan_id=3 -v folder_id=3 -f internal/db/explain_deleted_at_updates.sql
--   Or in psql: \set scan_id 3
--               \set folder_id 3
--               \i internal/db/explain_deleted_at_updates.sql
--
-- For large folders, run ANALYZE first so the planner has realistic row estimates:
--   ANALYZE files; ANALYZE file_scan;
--
-- Run in a single session; do not run multiple copies of this script concurrently,
-- or Section 2 can deadlock with another session updating the same files.

\echo '=== 0. Plan for "not in scan" row set (read-only; no UPDATE) ==='
EXPLAIN (ANALYZE, BUFFERS)
SELECT f.id FROM files f
WHERE f.folder_id = :folder_id
  AND NOT EXISTS (SELECT 1 FROM file_scan fs WHERE fs.scan_id = :scan_id AND fs.file_id = f.id);

\echo ''
\echo '=== 1. EXPLAIN (ANALYZE, BUFFERS) for "not in scan" UPDATE (then ROLLBACK) ==='
BEGIN;
EXPLAIN (ANALYZE, BUFFERS)
UPDATE files
SET deleted_at = COALESCE(deleted_at, (NOW() AT TIME ZONE 'UTC'))
WHERE folder_id = :folder_id
  AND NOT EXISTS (
    SELECT 1 FROM file_scan
    WHERE file_scan.scan_id = :scan_id AND file_scan.file_id = files.id
  );
ROLLBACK;

\echo ''
\echo '=== 2. EXPLAIN (ANALYZE, BUFFERS) for "in scan" UPDATE ==='
BEGIN;
EXPLAIN (ANALYZE, BUFFERS)
UPDATE files
SET
  deleted_at = NULL,
  hash_status = CASE WHEN (mtime IS DISTINCT FROM hashed_mtime OR hashed_mtime IS NULL) THEN 'pending' ELSE hash_status END,
  hash = CASE WHEN (mtime IS DISTINCT FROM hashed_mtime OR hashed_mtime IS NULL) THEN NULL ELSE hash END,
  hashed_at = CASE WHEN (mtime IS DISTINCT FROM hashed_mtime OR hashed_mtime IS NULL) THEN NULL ELSE hashed_at END,
  hashed_mtime = CASE WHEN (mtime IS DISTINCT FROM hashed_mtime OR hashed_mtime IS NULL) THEN NULL ELSE hashed_mtime END
FROM file_scan
WHERE files.id = file_scan.file_id AND file_scan.scan_id = :scan_id;
ROLLBACK;

\echo ''
\echo '=== 3. Row counts (no updates, read-only) ==='
\echo 'Files in folder:'
SELECT count(*) AS files_in_folder FROM files WHERE folder_id = :folder_id;
\echo 'Files in this scan (file_scan):'
SELECT count(*) AS files_in_scan FROM file_scan WHERE scan_id = :scan_id;
\echo 'Files in folder NOT in scan (candidate rows for "not in scan" update):'
SELECT count(*) AS not_in_scan
FROM files f
WHERE f.folder_id = :folder_id
  AND NOT EXISTS (SELECT 1 FROM file_scan fs WHERE fs.scan_id = :scan_id AND fs.file_id = f.id);
