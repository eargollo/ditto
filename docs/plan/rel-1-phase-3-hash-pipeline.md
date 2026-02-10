# Phase 3: Hash pipeline

**Goal:** Build the hash queue (same-size candidates only), priority ordering (size or size×count), bounded worker pool, SHA-256 hashing. Reuse known hash for hardlinks (same inode) and for unchanged files across scans (same inode+size). Throttle hashing. Scan runs to completion before hash starts (no concurrent scan+hash for now).

**References:** [ADR-005](../decisions/ADR-005-scan-and-hash-pipeline.md) (queue, priority, workers, throttle), [ADR-006](../decisions/ADR-006-hashing-and-duplicate-definition.md) (SHA-256, hardlinks), [ADR-007](../decisions/ADR-007-absolute-paths-and-scan-freshness.md) (queries scoped to scan_id).

---

## Execution model (how scan and hash run)

We do **not** pass work from scan to hash via in-process channels. The **database is the queue**: the scan phase writes file rows (with `hash_status = 'pending'`) into SQLite; the hash phase workers **pull** jobs by calling `ClaimNextHashJob`. That keeps the design simple, durable, and safe across restarts.

- **Scan then hash (no overlap for now):** We do **not** run scanning and hashing in parallel. The scan runs to completion (all file rows inserted, `completed_at` set), then the caller runs the hash phase for that `scan_id`. This avoids concurrency between the walk and the hash workers. Concurrent scan + hash may be considered later.
- **Scan:** One logical scan per root path (one `scan_id`). We can run scans **sequentially** (scan path A, then path B) or **in parallel** (one goroutine per path, each calling `RunScan` with a different root; each gets its own `scan_id`). No channel between scan and hash: when a scan completes, its rows are already in the DB for that `scan_id`.
- **Hash:** For a given `scan_id`, we run **N hashing workers** (goroutines). Each worker loops: `ClaimNextHashJob(ctx, db, scanID)` → reuse hash (same-scan inode or unchanged file from a previous scan) or read and hash → update row. When claim returns no job, the worker **exits** (no busy-loop). Coordination is via the DB (atomic claim); we don’t need a channel of jobs. We may use a **sync.WaitGroup** (or similar) so the caller can wait for “all workers finished”; optionally a small channel or callback to signal “hash phase complete” for the UI.
- **Channels:** Not required. Use **context.Context** for cancellation. Use channels only if we want a clear “phase done” or “scan_id ready for hash” signal (e.g. one goroutine per path that runs scan then enqueues `scan_id` onto a channel for a hash coordinator); that’s optional and can be done later (e.g. in the Web UI phase when we have “scan all roots” or progress reporting).

**One scan per path** (if we run multiple roots) and **multiple hashing workers per scan** are the intended model; the DB is the only “channel” between them.

---

## TDD and review

- Each step is implemented **test-first**: write the test(s), see them fail, then implement until they pass.
- One step = one logical change set. Prefer one PR per step.
- **Review checklist** per step: tests exist and pass; code is minimal; naming and layout match the plan.

---

## Step 1: Index and hash-queue query

**What:** Only files that share their size with at least one other file in the same scan are duplicate candidates (ADR-005). Add an index to support efficient “same-size group” queries. Provide a DB function that returns the next pending file to hash for a given scan, in priority order, and a way to “claim” it (set `hash_status = 'hashing'`) so only one worker processes it. The hash phase runs after the scan completes, so the candidate set is fixed when claiming; reuse (same-scan inode or previous-scan unchanged file) is decided when **processing** the claimed job (Steps 3 and 3b), not at claim time. **When multiple workers run in parallel, claim must be atomic:** a SELECT then UPDATE in one transaction is not enough — process 1 can SELECT row A, then process 2 SELECTs row A too before either updates; both would then UPDATE the same row. The claim must be a **single UPDATE statement** that both picks and marks the row (e.g. `UPDATE ... SET hash_status = 'hashing' WHERE id = (SELECT id FROM files WHERE ... pending ... LIMIT 1)`), so the engine updates exactly one row per execution and concurrent workers each get a different row. Use `RETURNING` (SQLite 3.35+) to get the claimed row back in one round-trip.

**TDD:**
- Test: insert files for a scan (e.g. two with size 100, one with size 200, one with size 300). Query “pending hash jobs” for that scan; assert only the two with size 100 are candidates (size 200 and 300 are unique, so not queued). Order by priority (e.g. size descending).
- Test: after setting one file’s `hash_status` to `'done'`, the other in the same size group is still a candidate until it is hashed.
- Test: claim next job returns one row and sets its `hash_status` to `'hashing'`; calling claim again does not return the same row (returns another or nothing).

**Deliverables:**
- Migration or `Migrate` update: add index on `(scan_id, size)` (if not already present) for efficient group-by-size and priority queries.
- `internal/db` (or `internal/hash` if you prefer a separate package): function to list or claim the next hash job for a scan. Priority: by **size** (largest first) or by **size × count** (total bytes in that size group); larger value = process first. For v1, one strategy is enough (e.g. size descending); document how to add the other.
- “Claim” = **single UPDATE statement** (not SELECT then UPDATE): e.g. `UPDATE files SET hash_status = 'hashing' WHERE id = (SELECT id FROM files WHERE scan_id = ? AND hash_status = 'pending' AND size IN (SELECT size FROM files WHERE scan_id = ? GROUP BY size HAVING COUNT(*) > 1) ORDER BY size DESC LIMIT 1) RETURNING id, path, size, inode, device_id, scan_id`. The subquery picks one row; the UPDATE marks it; only one row is updated per execution, so concurrent workers each get a different row. Requires SQLite 3.35+ for RETURNING (modernc.org/sqlite supports this).

**Review:** Unique-size files are never returned as jobs; claim is exclusive and atomic; priority order is correct; tests use in-memory DB.

---

## Step 2: SHA-256 hasher (pure function)

**What:** A function that reads a file by path and returns its SHA-256 hash (hex string or bytes). Use `crypto/sha256` and stream the file in chunks. No DB; no concurrency.

**TDD:**
- Test: write a temp file with known content (e.g. `"hello"`), hash it, assert result equals the known SHA-256 hex for that content.
- Test: empty file returns the correct SHA-256 of empty input.
- Test: non-existent path returns error (or skip if you only call from code that has just claimed a path).

**Deliverables:**
- `internal/hash` (or under `internal/scan` if you prefer): `HashFile(path string) (string, error)` returning hex-encoded SHA-256. Use `os.Open` and `io.Copy` into `sha256.New()` (or read in chunks). Close the file in all paths.

**Review:** No dependencies beyond stdlib; streaming for large files; tests are deterministic.

---

## Step 3: Hardlink hash reuse

**What:** For a given scan, if we have already hashed a file with the same (inode, device_id), do not read the path again — assign the existing hash to the new path and set `hash_status = 'done'`, `hashed_at = now` (ADR-006).

**TDD:**
- Test: insert two file rows for the same scan with the same inode and device_id (simulating hardlinks). Hash the first (via HashFile + UpdateFileHash). When “processing” the second, detect that this inode already has a hash in this scan; call a function that copies that hash to the second row and sets status/hashed_at. Assert second row has same hash as first and no second file read (mock or count reads).
- Test: same inode in a *different* scan does not reuse hash (different scan_id).

**Deliverables:**
- `internal/db`: `HashForInode(ctx, db, scanID, inode int64, deviceID *int64) (hash string, err error)` — returns the hash if any file in that scan with that (inode, device_id) already has a non-null hash; otherwise empty/not found.
- `internal/db`: `UpdateFileHash(ctx, db, fileID int64, hash string, hashedAt time.Time) error` — sets `hash`, `hash_status = 'done'`, `hashed_at` for the file.
- In the hash phase (next step): before calling `HashFile(path)`, call `HashForInode(...)`; if found, call `UpdateFileHash` for the current file with that hash and skip reading.

**Review:** Hardlinks reuse hash; no double read; same-scan only for this step.

---

## Step 3b: Reuse hash across scans (unchanged files)

**What:** Avoid re-hashing files whose content we already know. When we are about to hash a file (same scan: hardlink reuse already handled in Step 3), check whether we have a **previous** scan that hashed the same file — same (inode, device_id) and same **size** (and optionally same mtime). If we find a file row in the DB (any scan, or any scan other than the current one) with that inode, device_id, non-null hash, and same size, treat the file as unchanged and reuse that hash for the current row; skip reading. This way we only read and hash files that are new or have changed (different size or mtime).

**TDD:**
- Test: two scans. Scan 1 has file F (inode 123, size 100); we run hash phase for scan 1 and F gets hash "abc". Scan 2 has the same file F (inode 123, size 100 — unchanged). When processing a claimed job for F in scan 2, lookup "hash for (inode 123, device_id, size 100) from any other scan"; get "abc"; set current row to hash "abc" and skip HashFile. Assert only one read across both scans (or use a counter).
- Test: file changed size (or mtime if we use it) — do not reuse; hash the file.

**Deliverables:**
- `internal/db`: `HashForInodeFromPreviousScan(ctx, db, currentScanID int64, inode int64, deviceID *int64, size int64) (hash string, err error)` — returns the hash if any file row in the DB with a **different** scan_id has the same (inode, device_id), same size, and non-null hash; otherwise empty. Optionally add mtime to the match for stronger "unchanged" detection; for v1, inode + device_id + size is enough.
- In the hash phase: before calling `HashFile(path)`, (1) check same-scan inode (Step 3); (2) if not found, check previous-scan inode+size (this step); (3) if still not found, HashFile and UpdateFileHash.

**Review:** Unchanged files (same inode, size) reuse hash from a previous scan; no re-read. Changed files (different size) are hashed.

---

## Step 4: Run hash phase (single worker)

**What:** For a given scan_id, run the hash phase: repeatedly claim a job; if same-scan inode hash (Step 3) or previous-scan unchanged-file hash (Step 3b) is found, copy to this file; else read file, compute SHA-256, update file. Continue until there are no more pending candidates. At the start of the run, reset any `hash_status = 'hashing'` to `'pending'` for that scan so we recover from a previous crash. Optional: accept a context for cancellation.

**TDD:**
- Test: run a scan (phase 2), then run hash phase for that scan_id. Assert all files that are in same-size groups get `hash` and `hash_status = 'done'` and `hashed_at` set; files with unique size remain `pending` (and are never claimed).
- Test: two files same content and same size (duplicate candidates). After hash phase, both have the same hash.
- Test: two files same inode (hardlink). After hash phase, both have the same hash and only one file read (assert by mock or by counting HashFile calls in a wrapper).
- Test: context cancellation stops the loop and leaves remaining jobs still `pending` or `hashing` (document behavior).

**Deliverables:**
- `internal/hash` or `internal/scan`: `RunHashPhase(ctx, db, scanID int64, opts *HashOptions) error`. `HashOptions` can be nil; later add throttle (Step 6).
- Loop: reset `hashing` → `pending` for scan_id; then while (claim job): if HashForInode (same scan) returns hash, UpdateFileHash and continue; else if HashForInodeFromPreviousScan returns hash, UpdateFileHash and continue; else HashFile(path), UpdateFileHash. On claim failure (no row), exit loop.

**Review:** Only duplicate candidates are hashed; hardlinks reuse; single-worker flow is correct; cancellation is respected.

---

## Step 4b: Hash-phase metrics on scans (v1)

**What:** Store when hashing started and finished for a scan, and how much work was done (file count and bytes), so we can measure throughput (files/sec, bytes/sec) and tune parallelization. Add columns to the `scans` table; set them in `RunHashPhase`. v1: one set of metrics per scan (last hash run overwrites if we re-run).

**TDD:**
- Test: after running hash phase for a scan with some duplicate candidates, the scan row has `hash_started_at` and `hash_completed_at` set (completed >= started), `hashed_file_count` equal to the number of files that got `hash_status = 'done'`, and `hashed_byte_count` equal to the sum of `size` of those files.
- Test: if hash phase is not run for a scan, these columns remain null.

**Deliverables:**
- Migration: add to `scans` table: `hash_started_at` (TEXT, nullable), `hash_completed_at` (TEXT, nullable), `hashed_file_count` (INTEGER, nullable), `hashed_byte_count` (INTEGER, nullable). Use same datetime format as existing columns (e.g. RFC3339).
- At the start of `RunHashPhase(scanID)`: set `hash_started_at = now` for that scan (and optionally clear previous completed/counts if re-running).
- When all workers have finished (just before return): set `hash_completed_at = now`, `hashed_file_count` = count of files with `scan_id = ? AND hash_status = 'done'`, `hashed_byte_count` = sum of `size` for those files. DB helpers: e.g. `UpdateScanHashStartedAt`, `UpdateScanHashCompletedAt(ctx, db, scanID, fileCount, byteCount)` or a single update with all four fields at the end.
- Extend `Scan` struct and any scan getters to include the new fields (for UI or CLI later).

**Review:** Metrics are set only when hash phase runs; counts match actual hashed files; duration and throughput are derivable from the stored values.

---

## Step 5: Bounded worker pool

**What:** Run the hash phase with a configurable number of workers (e.g. default 4). Each worker claims jobs from the same queue (same scan_id) until no jobs remain. Claim must be atomic (Step 1) so concurrent workers never receive the same file; each worker does: claim → reuse or HashFile → update.

**TDD:**
- Test: run hash phase with 2 workers and a scan that has several same-size groups (e.g. 6 files in 3 size groups). Assert all 6 get hashed and each file is hashed exactly once (no duplicate work).
- Test: worker pool of 1 behaves like single-worker (same outcome as Step 4 test).

**Deliverables:**
- `HashOptions` (or config) gains `Workers int` (default e.g. 4). `RunHashPhase` starts N goroutines, each running the claim–hash–update loop; wait for all to finish when queue is empty (or use a shared “no more jobs” signal). Ensure safe concurrent claim (transaction in DB).
- Document that SQLite handles concurrent reads and one writer per transaction; avoid long-held transactions so workers don’t block each other.

**Review:** No double hashing; all candidates processed; pool size is respected.

---

## Step 6: Throttle hashing

**What:** Limit hashing to a configurable rate (e.g. max hashes per second) so the NAS I/O and CPU are not saturated (ADR-005). Use `golang.org/x/time/rate`: when `MaxHashesPerSecond > 0`, call `limiter.Wait(ctx)` before reading the file (before `HashFile`); when **0**, no throttle.

**TDD:**
- Test: with throttle enabled (e.g. 5 hashes/s), run hash phase on a scan with 3 duplicate candidates; assert elapsed time is at least ~400ms (two waits after first). Use generous margins.
- Test: with throttle disabled (`MaxHashesPerSecond == 0`), hash phase is fast (no artificial delay).

**Deliverables:**
- `HashOptions` gains `MaxHashesPerSecond int` (0 = no throttle). In each worker, before calling `HashFile(path)`, if opts.MaxHashesPerSecond > 0, call `limiter.Wait(ctx)`. Reuse the same pattern as Phase 2 scan throttle (one limiter per worker or shared limiter; shared is simpler for “N hashes per second total”).
- Optional: support “per-minute” or “bytes per second” in a later iteration; for v1, “hashes per second” is enough.

**Review:** Default 0 means full speed; rate limit is applied when set; tests use generous timing margins.

---

## Phase 3 done when

- [ ] All steps implemented with tests first (TDD).
- [ ] `go build ./...` and `go test ./...` pass.
- [ ] Reviewer has approved each step (or each PR).
- [ ] After a scan, running the hash phase for that scan_id fills in `hash` and `hashed_at` for all duplicate candidates (same-size groups); unique-size files remain pending; hardlinks reuse hash; worker pool and throttle work as configured.
- [ ] Scan row has hash-phase metrics (hash_started_at, hash_completed_at, hashed_file_count, hashed_byte_count) set after a hash run (Step 4b).

---

## Future: Recovery from system failure

**Not in scope for Phase 3**, but we will need a clear recovery story:

- **Problem:** A worker sets `hash_status = 'hashing'` when it claims a job. If the process or system dies before it sets `'done'` (or `'failed'`), that row stays in `'hashing'` forever and is never re-claimed, so the hash phase can never “finish” for that scan.
- **Options to consider later:**
  - **Reset on start:** At the beginning of each `RunHashPhase`, reset all rows for that scan with `hash_status = 'hashing'` back to `'pending'`. Simple and already mentioned in Step 4; any orphaned “hashing” from a previous run is retried. Downside: if we ever run multiple hash phases concurrently for different scans, we must not reset other scans’ rows.
  - **Timeout / lease:** Treat `'hashing'` as “claimed with a lease”; store a `claimed_at` (or `hash_started_at`) timestamp. On start (or periodically), reset to `'pending'` only rows that have been `'hashing'` longer than a threshold (e.g. 10 minutes). Allows long-running hashes without false resets; requires defining “stale” and possibly a background task.
  - **Run id or generation:** Each hash phase run gets a unique run id; workers only claim rows that are `'pending'` or that are `'hashing'` with an old run id. On restart, all `'hashing'` are considered stale and become claimable again by run id. More state; clearer semantics for “this run” vs “previous run.”

For now, **reset on start** (all `'hashing'` → `'pending'` for that scan at the beginning of `RunHashPhase`) is enough to recover from crashes and is explicitly part of Step 4. We can revisit a lease or run-id mechanism when we need concurrent runs or stricter guarantees.
