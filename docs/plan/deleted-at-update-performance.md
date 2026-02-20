# deleted_at update performance (large folders)

## Problem

After a scan, we run two bulk UPDATEs to set `files.deleted_at`:

1. **Not in scan:** set `deleted_at = now()` for files in the folder that are not in this scan.
2. **In scan:** set `deleted_at = NULL` (and reset hash fields when mtime changed) for files that are in this scan.

For folder 3 with **366,526 files**, observed times were:

- Not in scan: **~3 hours** (10,848,043 ms)
- In scan: **~55 s** (54,882 ms)

So the "not in scan" UPDATE dominates and is unacceptable.

## Root cause (from EXPLAIN)

On a DB where folder 3 had 0 files, the "not in scan" plan looked like:

- **Nested Loop Anti Join**, outer side: Index Scan on `files` with `folder_id = 3`, **estimated rows=1**.
- Inner side: Index Scan on `file_scan` with `scan_id = 3`.

The planner assumed the outer side had 1 row, so it chose a nested loop. In production the outer side has **366k rows**, so the loop runs 366k times (one index probe into `file_scan` per file) → ~3 hours. The plan is wrong because **row estimates were way off** (stale or empty stats for that folder).

**Fix:** Give the planner correct statistics and a better index so it can choose a Hash Anti Join (or at least a cheap inner side):

1. **Run `ANALYZE files; ANALYZE file_scan;`** (or let autovacuum do it). With realistic stats, the planner should see hundreds of thousands of rows and prefer **Hash Anti Join** (build a hash of `file_scan.file_id` for `scan_id = 3`, then scan `files` with `folder_id = 3` and probe) — O(n + m) instead of O(n × log m).
2. **Add index `file_scan(scan_id, file_id)`.** This speeds up the hash build (or the inner index scan if the planner still uses nested loop) and helps the "in scan" UPDATE.

After applying the migration and running ANALYZE, re-run the EXPLAIN script on the real DB (or a copy with similar row counts) and confirm the plan shows Hash Anti Join and reasonable execution time.

**Result (366k files, after ANALYZE):** The "not in scan" UPDATE plan is **Hash Anti Join** with Seq Scan on `files` and Seq Scan on `file_scan`; **Execution Time ~1.8 s** when 0 rows are updated (first full scan). So the fix is correct stats (ANALYZE); the previous ~3 h run was due to the wrong plan (Nested Loop) from bad estimates. Adding the index `file_scan(scan_id, file_id)` is still recommended so the hash build uses an index scan instead of a seq scan on `file_scan`, which will scale better as the table grows.

### Why the 3 h is fixed

The slowness was **not** the query text or the index. It was the **plan choice**:

- **Bad stats** → planner estimated 1 row for “files in folder 3” → it chose **Nested Loop Anti Join** (cheap for 1 row: one lookup). In reality there were 366k rows, so the loop ran **366k times** (~3 h).
- **Good stats (ANALYZE)** → planner sees ~366k rows → it chooses **Hash Anti Join**: build a hash of the 366k `file_id`s from `file_scan`, then scan the 366k `files` and probe. That’s one pass over each table → **~2–4 s**.

So the fix is **up-to-date statistics**. The index helps other queries and keeps the plan robust; it didn’t fix the 3 h by itself.

### Expected duration (with good stats)

| Step | What it does | Typical time (366k files) |
|------|----------------|----------------------------|
| Not in scan | Find files in folder not in this scan, set `deleted_at = now()` | **~2–5 s** (0 rows when first full scan; a bit more if many rows updated) |
| In scan | Set `deleted_at = NULL` (+ hash resets) for all files in this scan | **~55 s** (you already saw this; scales with row count) |
| **Total** | | **~1 min** (about 1 min for a 366k-file folder with good stats) |

If the folder has fewer files, total time is lower (e.g. 16k files → “not in scan” &lt;1 s, “in scan” ~2 s). If “not in scan” updates many rows (lots of files removed since last scan), add a few seconds to a minute. You should **not** see multiple minutes for “not in scan” anymore; if you do, run `ANALYZE files; ANALYZE file_scan;` and re-check the plan (should be Hash Anti Join).

## Analysis

1. Run the query analyzer (no schema change required):
   ```bash
   psql "$DATABASE_URL" -v scan_id=3 -v folder_id=3 -f internal/db/explain_deleted_at_updates.sql
   ```
2. Check in the EXPLAIN (ANALYZE, BUFFERS) output:
   - **Not in scan:** Look for Seq Scan vs Index Scan, nested loop vs hash anti-join, and buffer hit/miss. How many rows are actually updated?
   - **In scan:** Same; confirm whether the JOIN uses an index on `file_scan(scan_id, file_id)` (or equivalent).

3. Use the row-count section at the end to see:
   - `files_in_folder` vs `files_in_scan` vs `not_in_scan`. If `not_in_scan` is huge, that explains a lot of work.

## Index (already planned)

- Add `idx_file_scan_scan_id_file_id` on `file_scan(scan_id, file_id)` so the "not in scan" NOT EXISTS and the "in scan" JOIN can use it. Apply only after confirming with EXPLAIN that it helps.

## Alternative approaches (if one big UPDATE stays slow)

### A. Batch "not in scan" by file id ranges

- `SELECT id FROM files WHERE folder_id = $1 AND NOT EXISTS (...) ORDER BY id` in a cursor or in chunks (e.g. `WHERE id > $last_id ORDER BY id LIMIT 10000`).
- Then `UPDATE files SET deleted_at = now() WHERE id = ANY($batch)` in batches of 5k–20k. Commits progress and reduces lock time per round.

### B. UPDATE from a materialized set

- `WITH to_update AS (SELECT f.id FROM files f WHERE f.folder_id = $1 AND NOT EXISTS (...)) UPDATE files SET deleted_at = now() WHERE id IN (SELECT id FROM to_update)`.
- Sometimes the planner does better with an explicit CTE; EXPLAIN will tell.

### C. Invert: mark all in folder deleted, then unmark in-scan

- `UPDATE files SET deleted_at = now() WHERE folder_id = $1` (one index scan on `folder_id`).
- Then run the existing "in scan" UPDATE (`deleted_at = NULL` + hash resets for those in `file_scan`).
- Pro: first UPDATE is a simple index scan + update. Con: we overwrite `deleted_at` for files that are in the scan (then the second UPDATE fixes them). More rows touched in the first step; only worth it if the second step is cheap and the combined time is better. Measure with EXPLAIN ANALYZE.

### D. Temporary table

- `CREATE TEMP TABLE not_in_scan AS SELECT f.id FROM files f WHERE f.folder_id = $1 AND NOT EXISTS (...); CREATE INDEX ON not_in_scan(id); UPDATE files SET deleted_at = now() WHERE id IN (SELECT id FROM not_in_scan);`
- Lets the planner use a small driver table; may improve the UPDATE plan.

Next step: run the analyzer script, paste or summarize the EXPLAIN output and the three counts, then choose index vs batching vs another approach.

---

## Full analysis of EXPLAIN output (366k files, folder 3)

Example run: `psql "$DATABASE_URL" -v scan_id=3 -v folder_id=3 -f internal/db/explain_deleted_at_updates.sql` after `ANALYZE files; ANALYZE file_scan;`.

### Section 0 — Read-only SELECT (“not in scan” row set)

- **Plan:** Gather → **Parallel Hash Anti Join** (2 workers).
  - Outer: **Parallel Seq Scan on files** with `Filter: (folder_id = 3)` → 366,526 rows (122,175 × 3 workers), **actual time ~16.9 s** (slowest worker).
  - Inner: **Parallel Hash** from **Parallel Seq Scan on file_scan** with `Filter: (scan_id = 3)` → 366,526 rows, hash build ~112 ms, scan ~37 ms.
- **Buffers:** `shared hit=6076 read=12883` — large amount of **disk read** (12,883 blocks), so this run was I/O-bound.
- **Execution Time: 17,243 ms (~17 s).**
- **Why slower than Section 1:** Parallel workers each seq-scan a slice of the table; with cold cache you pay for reading both tables from disk. The SELECT returns 0 rows (all files are in the scan) but still has to evaluate the anti-join over the full sets.

### Section 1 — “Not in scan” UPDATE (then ROLLBACK)

- **Plan:** **Hash Anti Join** (no parallelism; UPDATE doesn’t use parallel workers here).
  - Outer: **Seq Scan on files** with `Filter: (folder_id = 3)` → 366,526 rows, **584 ms**.
  - Inner: **Hash** from **Seq Scan on file_scan** with `Filter: (scan_id = 3)` → 366,526 rows, scan 93 ms, hash build 840 ms.
- **Buffers:** `shared hit=6977 read=11982` (some disk read), `temp read=2420 written=2420` (hash spilled to temp).
- **Execution Time: 1,845 ms (~1.8 s).**
- **Rows updated: 0** (first full scan; every file in the folder is in the scan).
- **Takeaway:** With good stats the planner uses Hash Anti Join; the “not in scan” UPDATE is **~1.8 s** instead of ~3 hours. The earlier 3-hour run was due to the wrong plan (Nested Loop) from bad estimates.

### Section 2 — “In scan” UPDATE

- In the example run, Section 2 failed with **deadlock detected**: two processes (e.g. two psql sessions running the script, or one script and another writer) were both updating `files` and blocked each other. This is **not** a problem with the query plan; it’s concurrency.
- **To get a clean EXPLAIN for “in scan”:** Run the script in a **single session** with no other concurrent updates to `files`, or run only Section 2 in isolation (e.g. a one-off `BEGIN; EXPLAIN (ANALYZE, BUFFERS) UPDATE ... FROM file_scan ... ; ROLLBACK;`).
- From production logs we already know “in scan” for 366k rows took **~55 s** (54,882 ms). The plan is a single UPDATE that joins `files` to `file_scan` on `scan_id = 3` and updates 366k rows; cost is dominated by locking and writing 366k rows.

### Section 3 — Row counts

| Metric | Value |
|--------|--------|
| files_in_folder | 366,526 |
| files_in_scan | 366,526 |
| not_in_scan | 0 |

So this is a **first full scan**: every file in the folder is in the scan, so the “not in scan” UPDATE touches 0 rows. When many files have been removed from disk since the last scan, `not_in_scan` would be large and the same “not in scan” UPDATE would still use the Hash Anti Join but would then write many rows (still expected to be in the ~seconds to low tens of seconds range with good stats and the index).

### Summary

| Step | Plan | Time (this run) | Notes |
|------|------|------------------|--------|
| 0 – SELECT “not in scan” | Parallel Hash Anti Join | ~17 s | Read-only; I/O-bound (disk read). |
| 1 – UPDATE “not in scan” | Hash Anti Join | **~1.8 s** | 0 rows updated; fix for the former 3 h run. |
| 2 – UPDATE “in scan” | (not captured; deadlock) | ~55 s in prod | 366k rows updated; run script alone to get EXPLAIN. |
| 3 – Counts | — | — | 366k / 366k / 0 → first full scan. |

**Recommendations:** Keep ANALYZE up to date. Apply the index `file_scan(scan_id, file_id)` so both the “not in scan” hash build and the “in scan” JOIN can use it. Run the EXPLAIN script in a single connection and avoid concurrent updates when capturing Section 2.

### After adding the index (empty or small DB)

If you run the EXPLAIN script **after** applying migration `000002` (index `file_scan(scan_id, file_id)`) on a DB where **folder 3 and scan 3 have 0 rows** (e.g. fresh test DB or different folder/scan), you will see:

- **Section 0 & 1:** **Nested Loop Anti Join**, estimated `rows=1`, execution &lt; 1 ms. The planner chooses nested loop because the stats say the result set is tiny (0 rows).
- **Section 2:** **Nested Loop** (not Hash Join), Index Scan on `idx_file_scan_scan_id` and `files_pkey`, &lt; 1 ms.
- **Section 3:** `files_in_folder=0`, `files_in_scan=0`, `not_in_scan=0`.

So the plan looks like the "bad" plan (Nested Loop) again — but only because the data is empty. The planner is correct for this size. The composite index may still appear as `idx_file_scan_scan_id` in the plan when the planner prefers the existing single-column index for tiny scans.

**On the large DB (366k files):** After ANALYZE, the planner will use **Hash Anti Join** for "not in scan". Re-run the script on the real DB after migration + ANALYZE to confirm.

**Observed after index on large DB (366k files):** Section 0: **~6.3 s** (down from ~17 s; Parallel Hash Anti Join, still Seq Scan on both tables — likely better cache). Section 1: **Hash Anti Join**, **~4 s** (plan still shows Seq Scan on `file_scan`, not the new index; for "all rows with scan_id = 3" the planner may prefer Seq Scan. Time can vary with load/cache; earlier run was ~1.8 s). Section 2: deadlock again with two processes. Counts: 366k / 366k / 0. The critical fix is unchanged: **Hash Anti Join** is used, so the former ~3 h Nested Loop problem is resolved. The composite index is available for other queries or if the planner chooses it in the future.
