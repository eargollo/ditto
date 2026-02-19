# Precompute duplicate groups + single "all duplicates" view

**Goals:** Precompute duplicate groups so the landing page is fast; simplify the UI by removing the scan dropdown and always showing "all duplicates" (latest completed scan per folder); expose useful aggregates for the user (number of duplicate groups, total duplicated files, total duplicated data, reclaimable space).

---

## UI simplification

- **Remove** the folder/scan dropdown from the home page. There is no "per scan" view anymore — only "all duplicates" (based on latest completed scan per folder).
- **Landing page** shows one view: duplicate groups by content (hash), with optional section for hardlinks (inode groups) if desired. Links to group detail use e.g. `/duplicates/hash/{hash}` (no `scan_id` in URL).
- **Scans page** (`/scans`) remains for starting scans and seeing progress; it does not drive which duplicates are shown. Duplicates are always "current" (precomputed from latest per folder).

---

## Precompute table design

### 1. `duplicate_groups_hash`

One row per duplicate-by-content group (hash with more than one file in "current" view).

| Column            | Type    | Meaning |
|-------------------|---------|---------|
| hash              | TEXT    | Content hash (e.g. SHA-256). Primary key. |
| file_count        | BIGINT  | Number of files in this group. |
| total_size        | BIGINT  | Sum of sizes of all files in the group (same as today's `Size`). |
| reclaimable_size  | BIGINT  | Space that could be reclaimed by keeping one copy: `total_size * (file_count - 1) / file_count`. Enables a single `SUM(reclaimable_size)` for the landing summary. |

### 2. `duplicate_groups_inode`

One row per duplicate-by-inode group (hardlinks).

| Column            | Type    | Meaning |
|-------------------|---------|---------|
| inode             | BIGINT  | Inode number. |
| device_id         | BIGINT  | Device ID (nullable; use e.g. -1 in PK when NULL). |
| file_count        | BIGINT  | Number of hardlinked files. |
| total_size        | BIGINT  | Sum of sizes. |
| reclaimable_size  | BIGINT  | Same semantics as hash table (for consistency). |

Primary key: `(inode, COALESCE(device_id, -1))` or equivalent so `device_id` NULL is one key.

### 3. `current_duplicate_scans`

Which scan_ids the precompute is based on (so "files in this group" can still be resolved when the user clicks a group).

| Column   | Type   | Meaning |
|----------|--------|---------|
| scan_id  | BIGINT | A scan that is "current" (latest completed per folder). Primary key. |

One row per scan_id. On each precompute run: `TRUNCATE current_duplicate_scans` then `INSERT` the set of "latest completed scan per folder". Optionally add a small table e.g. `precompute_metadata(computed_at)` if you want "last updated" in the UI.

---

## How "current" scan set is defined

- **Latest completed scan per folder** = one scan per `folder_id` with `hash_completed_at IS NOT NULL`, choosing the most recent by `started_at DESC` (or `id DESC`). Same semantics as today's "All (latest per folder)".
- **Query (Postgres):**  
  `SELECT DISTINCT ON (folder_id) id FROM scans WHERE hash_completed_at IS NOT NULL ORDER BY folder_id, started_at DESC, id DESC`
- Expose as e.g. `db.LatestCompletedScanIDsPerFolder(ctx, db)`.

---

## Precompute job: when and what

**When:** After the hash phase completes for a scan ([internal/hash/run.go](internal/hash/run.go) — after `UpdateScanHashCompletedAt`). Optionally also on first request if the precompute tables are empty (e.g. after deploy).

**What:**

1. Compute current scan_ids (latest completed per folder).
2. `TRUNCATE duplicate_groups_hash`, `duplicate_groups_inode`, `current_duplicate_scans`.
3. Run the same aggregation as today: `files` ⋈ `file_scan` WHERE `scan_id IN (current_scan_ids)` AND `hash_status = 'done'`, GROUP BY hash HAVING COUNT(*) > 1. Insert `(hash, file_count, total_size, reclaimable_size)` into `duplicate_groups_hash`. Same for inode groups into `duplicate_groups_inode`. (`reclaimable_size = total_size * (file_count - 1) / file_count`.)
4. Insert the current scan_ids into `current_duplicate_scans`.

Precompute runs once per hash-phase completion. No per-request heavy join.

---

## Landing page: data from precompute tables

**Summary (for the user at the top of the page):**

- **Total duplicate groups:** `SELECT COUNT(*) FROM duplicate_groups_hash`
- **Total duplicated files:** `SELECT COALESCE(SUM(file_count), 0) FROM duplicate_groups_hash`
- **Total duplicated data:** `SELECT COALESCE(SUM(total_size), 0) FROM duplicate_groups_hash` (how much space is used by duplicate content)
- **Reclaimable space:** `SELECT COALESCE(SUM(reclaimable_size), 0) FROM duplicate_groups_hash` (space you could free by keeping one copy per group)

**Paginated list of groups:**  
`SELECT hash, file_count, total_size, reclaimable_size FROM duplicate_groups_hash ORDER BY total_size DESC LIMIT $1 OFFSET $2`  
No join to `files` or `file_scan`.

**Sample paths per group (for each card):**  
Still need a live query: `FilesInHashGroupLimitAcrossScans(ctx, db, scanIDs, hash, N)` where `scanIDs = SELECT scan_id FROM current_duplicate_scans`. One small query to get scan_ids, then one query per group on the page (or a batched "paths for these hashes" query to reduce N+1).

---

## Group detail (files in one group)

When the user clicks a group (e.g. `/duplicates/hash/{hash}`):

1. Get scan_ids from `SELECT scan_id FROM current_duplicate_scans`.
2. Call `FilesInHashGroupLimitAcrossScans(ctx, db, scanIDs, hash, 0)` to list all files.

Only the source of scan_ids changes (from "latest per folder" computed on each request to "stored in current_duplicate_scans"). The existing file-listing query stays the same.

---

## Files to touch (summary)

- **Schema:** [internal/db/pg.go](internal/db/pg.go) (or a migration): add `duplicate_groups_hash`, `duplicate_groups_inode`, `current_duplicate_scans`; optionally `precompute_metadata(computed_at)` for "last updated" in the UI.
- **DB layer:** New functions: `LatestCompletedScanIDsPerFolder`, `PrecomputeDuplicateGroups` (run aggregation and fill the three tables), read from precompute tables (list + summary), read `current_duplicate_scans` for scan_ids when listing files in a group.
- **Hash phase:** After [internal/hash/run.go](internal/hash/run.go) `UpdateScanHashCompletedAt`, call the precompute job (sync or async; async avoids blocking the hash-phase response).
- **Server:** Home handler reads from precompute tables and summary; no dropdown; no `scan_id` in duplicate URLs. Group-detail handler takes hash (and inode/device_id for inode groups) and uses `current_duplicate_scans` for scan_ids.
- **Templates:** [internal/server/templates/home.html](internal/server/templates/home.html) — remove the form with the scan select; add a line or card showing "X duplicate groups, Y files, Z MB duplicated, W MB reclaimable" from the new summary. Links become e.g. `/duplicates/hash/{{.Hash}}`.
- **Routes:** Add `/duplicates/hash/{hash}` (and optionally `/duplicates/inode?...`). Keep or redirect `/scans/0/duplicates/...` to the new paths if you want backward compatibility.
