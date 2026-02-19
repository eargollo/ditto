# Plan: deleted_at on files + precompute duplicate_groups_hash

**Goal:** (1) Exclude deleted files from duplicate views without joining the ledger at read time. (2) Precompute duplicate groups into a single table so the landing page is fast. (3) Simplify UI to a single "all duplicates" view (no scan dropdown).

**Approach:** Add `deleted_at` to `files`; update it in bulk when a scan completes. List files and precompute using only `files` (and `duplicate_groups_hash`) with `deleted_at IS NULL` — no join to `file_scan` for duplicate UX.

---

## 1. Schema changes

### 1.1 Add `deleted_at` to `files`

- **Table:** `files`  
- **Column:** `deleted_at TIMESTAMPTZ NULL`  
- **Meaning:** `NULL` = file is considered present (show in duplicates and file lists). Non-NULL = file is considered deleted (exclude from duplicates and file lists).  
- **Where:** [internal/db/pg.go](internal/db/pg.go) in `MigratePostgres`: add `ALTER TABLE files ADD COLUMN deleted_at TIMESTAMPTZ` (idempotent: ignore if column exists), or a dedicated migration step.

### 1.2 Add table `duplicate_groups_hash`

- **Columns:**  
  - `hash TEXT PRIMARY KEY`  
  - `file_count BIGINT NOT NULL`  
  - `total_size BIGINT NOT NULL`  
  - `reclaimable_size BIGINT NOT NULL` (optional for UI: space reclaimable by keeping one copy)  
- **Where:** Same migration / `MigratePostgres`.  
- **Filled by:** Precompute job (see below). No foreign keys; standalone summary table.

### 1.3 Add `deleted_at_update_duration_ms` to `scans`

- **Table:** `scans`  
- **Column:** `deleted_at_update_duration_ms BIGINT NULL`  
- **Meaning:** Duration in milliseconds of the bulk `deleted_at` update run for this scan (set right after that update). Enables monitoring and UI display (e.g. on the scans page).  
- **Where:** Same migration as above; add column to the `scans` DDL or `ALTER TABLE scans ADD COLUMN deleted_at_update_duration_ms BIGINT` (idempotent).

### 1.4 Index for duplicate reads (optional but useful)

- Index on `files` to support "list files by hash and non-deleted": e.g. `(hash, deleted_at)` or `(hash)` with a partial index `WHERE hash_status = 'done' AND deleted_at IS NULL`. Add in migration if needed after measuring.

---

## 2. Bulk update of `deleted_at` when a scan completes

**When:** Immediately after the scan phase completes for a given scan (all `file_scan` rows for that scan are already inserted). So: after `UpdateScanCompletedAt` in the scan run path.

**Where to call:**  
- [internal/scan/run.go](internal/scan/run.go): after `UpdateScanCompletedAt` in `RunScan` and in `RunScanForExisting`, call a new DB function that performs the bulk update for that scan.  
- Need `folder_id` for the scan: get it from the scan row (or pass it; we have it in both call sites).

**Logic (single UPDATE or two):**

- For scan N (folder F):  
  - Set `deleted_at = NULL` for every file in folder F that appears in `file_scan` for scan N (they (re)appeared).  
  - Set `deleted_at = now()` for every file in folder F that does **not** appear in `file_scan` for scan N.

**Example (one statement):**

```sql
UPDATE files
SET deleted_at = CASE
  WHEN id IN (SELECT file_id FROM file_scan WHERE scan_id = $1) THEN NULL
  ELSE COALESCE(deleted_at, (NOW() AT TIME ZONE 'UTC'))
END
WHERE folder_id = $2;
```

Parameters: `$1 = scanID`, `$2 = folderID` (from scan row).

**New DB functions:**  
- `UpdateFilesDeletedAtForScan(ctx, db, scanID int64) error` in [internal/db/files.go](internal/db/files.go) (or a new file like `internal/db/files_deleted.go`). It loads the scan’s `folder_id` (or caller passes it), then runs the UPDATE. Keep it a single bulk update (or two simple UPDATEs) so it stays efficient.  
- `UpdateScanDeletedAtUpdateDuration(ctx, db, scanID int64, durationMs int64) error` in [internal/db/scans.go](internal/db/scans.go): `UPDATE scans SET deleted_at_update_duration_ms = $1 WHERE id = $2`. Used to persist the duration of the `deleted_at` update on the scan row.

**Recording duration:** In [internal/scan/run.go](internal/scan/run.go), after `UpdateScanCompletedAt`, measure the time around `UpdateFilesDeletedAtForScan` and then call `UpdateScanDeletedAtUpdateDuration` with the elapsed milliseconds (e.g. `time.Since(start).Milliseconds()`). Optionally log the duration (e.g. `log.Printf("[scan] deleted_at update for scan %d took %d ms", scanID, durationMs)`).

---

## 3. Precompute job: fill `duplicate_groups_hash`

**When:** After the hash phase completes for a scan (so hashes are available). Call after `UpdateScanHashCompletedAt` in [internal/hash/run.go](internal/hash/run.go).

**Logic:**  
- No join to `file_scan`.  
- Query: from `files` where `hash_status = 'done'` and `deleted_at IS NULL`, group by `hash` with `HAVING COUNT(*) > 1`, compute `file_count`, `total_size`, `reclaimable_size = total_size * (file_count - 1) / file_count`.  
- Then: `TRUNCATE duplicate_groups_hash`; `INSERT` the result set into `duplicate_groups_hash`.

**New DB functions (e.g. in [internal/db/duplicates.go](internal/db/duplicates.go) or new `internal/db/precompute.go`):**  
- `PrecomputeDuplicateGroupsHash(ctx, db) error` — runs the aggregation and truncate/insert.  
- Optionally: `DuplicateGroupsHashSummary(ctx, db)` returning (groupCount, totalFiles, totalSize, reclaimableSize) for the landing page; and `DuplicateGroupsHashPaginated(ctx, db, limit, offset)` reading from `duplicate_groups_hash` for the list.  
- **List files in a group:** new (or refactored) function that queries only `files` (+ folders for path): `FilesInHashGroup(ctx, db, hash string)` → `SELECT ... FROM files f JOIN folders fo ON f.folder_id = fo.id WHERE f.hash = $1 AND f.hash_status = 'done' AND f.deleted_at IS NULL ORDER BY f.path`. No `file_scan`.

---

## 4. Backfill of `deleted_at` for existing data

**When:** Once, after adding the column (e.g. in the same migration or a follow-up migration / startup step).

**Logic:** For each folder, determine the “current” scan (e.g. latest scan for that folder with `hash_completed_at IS NOT NULL`, or latest with `completed_at IS NOT NULL` if we’re okay with pre-hash state). Then:  
- Set `deleted_at = NULL` for files in that folder that are in `file_scan` for that scan.  
- Set `deleted_at = now()` for files in that folder that are not in that scan.

Can be implemented as a SQL script or a Go function run once (e.g. from a migration or a one-off step). After that, normal scan completion keeps `deleted_at` correct.

---

## 5. UI and server changes

### 5.1 Landing page (home) layout

**Summary at top:** One block with three metrics: (1) total duplicate groups, (2) total duplicated files, (3) total size that can be saved (reclaimable). Data from one query to `duplicate_groups_hash`: `COUNT(*)`, `SUM(file_count)`, `SUM(reclaimable_size)` (e.g. `DuplicateGroupsHashSummary`).

**List of groups below:** For each duplicate group (paginated if needed): (1) **Group data** — hash (or truncated), file count, total size, reclaimable size for that group; (2) **Group list of files** — the file paths (and optionally size) for that group, loaded via `FilesInHashGroup(ctx, db, hash)` (with limit on landing page; link to group detail for full list). Page structure: **[Summary: groups | files | size that can be saved]** then **[Group 1: stats + file list]** then **[Group 2: stats + file list]** etc.

Remove the folder/scan dropdown. Links to full group: `/duplicates/hash/{hash}`.

### 5.2 Group detail page


- Route: e.g. `GET /duplicates/hash/{hash}`.  
- Shows group data (hash, file count, total size, reclaimable) and the full list of files via `FilesInHashGroup(ctx, db, hash)`.  
- Remove or redirect old `/scans/{id}/duplicates/...` for the all flow.

### 5.3 Scans page and templates

- **Scans page:** Unchanged for starting scans and progress. Optionally show `deleted_at_update_duration_ms` per scan. No dropdown there for “which scan’s duplicates”; duplicates are always the precomputed “all.”

- **Templates:** [internal/server/templates/home.html](internal/server/templates/home.html) — remove the scan select form; add summary block at top (three metrics: duplicate groups, duplicated files, size that can be saved); then list of groups, each with group data and that group's file list; links to `/duplicates/hash/{{.Hash}}`.

---

## 6. Execution order (implementation)

| Step | What |
|------|------|
| 1 | Schema: add `files.deleted_at`, add `scans.deleted_at_update_duration_ms`, create `duplicate_groups_hash` table (and optional index on `files`). |
| 2 | Schema: add `scans.deleted_at_update_duration_ms`. DB: implement `UpdateFilesDeletedAtForScan` and `UpdateScanDeletedAtUpdateDuration`; from scan completion (RunScan / RunScanForExisting) call the former, measure duration, then call the latter. |
| 3 | Backfill: one-time set of `deleted_at` from current “latest scan per folder” (or latest completed scan per folder). |
| 4 | DB: implement precompute (aggregate from `files` with `deleted_at IS NULL`), `FilesInHashGroup` by hash only, and summary/paginated reads from `duplicate_groups_hash`. |
| 5 | Hash phase: after `UpdateScanHashCompletedAt`, call `PrecomputeDuplicateGroupsHash`. |
| 6 | Server: home handler uses precompute + new `FilesInHashGroup`; add route `/duplicates/hash/{hash}`; remove scan dropdown and update links. |
| 7 | Optional: remove or redirect old duplicate routes that depended on scan_id for “all” view. |

---

## 7. Files to touch (summary)

- **Schema/migration:** [internal/db/pg.go](internal/db/pg.go) — `deleted_at` on `files`, `deleted_at_update_duration_ms` on `scans`, create `duplicate_groups_hash`; optional index on `files`.
- **DB layer:**  
  - [internal/db/files.go](internal/db/files.go) (or new file): `UpdateFilesDeletedAtForScan`.  
  - [internal/db/scans.go](internal/db/scans.go): `UpdateScanDeletedAtUpdateDuration(ctx, db, scanID, durationMs)`. Extend `Scan` struct and `GetScan` (and list scans queries if needed) to include `DeletedAtUpdateDurationMs *int64` for UI.  
  - [internal/db/duplicates.go](internal/db/duplicates.go) or new `precompute.go`: `PrecomputeDuplicateGroupsHash`, `DuplicateGroupsHashSummary`, `DuplicateGroupsHashPaginated`, `FilesInHashGroup(ctx, db, hash)` (by hash only, `deleted_at IS NULL`).  
  - Backfill: migration or one-off.
- **Scan:** [internal/scan/run.go](internal/scan/run.go) — after `UpdateScanCompletedAt`, call `UpdateFilesDeletedAtForScan`, measure duration, then `UpdateScanDeletedAtUpdateDuration`.  
- **Hash:** [internal/hash/run.go](internal/hash/run.go) — after `UpdateScanHashCompletedAt`, call `PrecomputeDuplicateGroupsHash`.  
- **Server:** [internal/server/server.go](internal/server/server.go) — home handler, new route, remove scan_id from duplicate flow.  
- **Templates:** [internal/server/templates/home.html](internal/server/templates/home.html) — remove dropdown, add summary, update links.

---

## 8. What we do *not* add

- No `current_duplicate_scans` table — we don’t need scan_ids for listing files; we use `deleted_at IS NULL` and hash.  
- No `duplicate_groups_inode` table for this phase — focus on hash-based duplicates only.  
- No join to `file_scan` for duplicate listing or precompute — only for the bulk `deleted_at` update at scan completion.
