# Integration / regression test plan

Goals: verify end-to-end behavior of scan → hash → duplicate grouping, and that per-scan statistics (hashed, reused, read from disk) match reality. Tests should run against a real DB (Postgres) and a real directory tree (temp dir with controlled files).

**Design principle: duplicates across all folders.** The product has multiple folders (each scanned separately). Duplicate detection is **global**: we find duplicates among all scanned files from all folders, not per folder. A duplicate group can contain files from different folders (e.g. scenario 7: A1, B1 in folder 1 and E2 in folder 2). The hash phase uses a global duplicate-size queue so that when a new scan adds files whose size already exists in another folder, we hash those other-folder files too and form cross-folder groups.

For each scenario we validate:
1. **Grouping** – duplicate groups (by hash), file membership, counts, reclaimable size; summary totals.
2. **Per-scan statistics** – for each scan: files scanned, files with hash, reused (no read), read from disk, hash errors; hashed bytes where relevant.

## Summary table

| Scenario                      | Grouping check                          | Scan 1 stats (main)     | Scan 2 stats (main)     |
|------------------------------|------------------------------------------|-------------------------|-------------------------|
| 1. First scan, duplicates   | 2 groups (A,B and C,D); reclaimable      | 5 scanned; 4 hashed; 0 reused; 4 read | —                        |
| 2. Second scan, no changes   | Same 2 groups                            | (unchanged)             | 4 hashed; 4 reused; 0 read |
| 3. One duplicate changes (same size) | 1 group (C,D); A,B no longer duplicate | (unchanged)             | 4 hashed; 3 reused; 1 read |
| 4. One duplicate changes (diff size, unique) | 1 group (C,D); B hash IS NULL | (unchanged)             | 3 hashed; 3 reused; 0 read; B not hashed |
| 5. One file becomes duplicate (E→A) | 1 group of 3 (A,B,E), 1 of 2 (C,D)     | (unchanged)             | 5 hashed; 4 reused; 1 read |
| 6. Delete B, D between scans | 0 groups (no pairs among live files)    | (unchanged)             | 3 scanned; 2 hashed; 2 reused; 0 read (E unique size) |
| 7. Two folders: scan 1 then 2 | 2 groups (A1,B1,E2) and (C2,D2); cross-folder + within-folder | 2 scanned; 2 hashed; 0 reused; 2 read | 4 scanned; 4 hashed; 0 reused; 4 read |
| 8. Two folders: first no dup, second match + same-size-diff | 1 group (P1,R2,R2b); scan 1 files hashed when scan 2 runs (global queue); S2 same size as Q1 but different | 2 scanned; 2 hashed; 0 reused; 2 read | 3 scanned; 3 hashed; 0 reused; 3 read |

---

## 1. First scan with some duplicates

**Setup**
- Temp dir with e.g. 5 files: A, B, C, D, E.
- A and B identical (same content → same size, same hash).
- C and D identical (different from A/B).
- E unique (no duplicate).

**Actions**
- Run scan on the folder.
- Run hash phase (or let pipeline complete).

**Assert – grouping**
- Exactly 2 duplicate groups (by hash).
- Group 1: 2 files (A, B), reclaimable = one file size.
- Group 2: 2 files (C, D), reclaimable = one file size.
- E appears in no group (or only in a “group” of 1 if we expose that; otherwise just not in any multi-file group).
- Summary: group count 2, total files 4 in groups, total reclaimable = 2 × (one file size).

**Assert – scan 1 stats**
- Files scanned: 5.
- Files with hash: 4 (only duplicate-size candidates get hashed; E has unique size so may not be hashed depending on rules – if we only hash sizes that appear 2+ times globally, then 4).
- Reused: 0 (first scan).
- Read from disk: 4 (or 0 for E if not queued).
- Hash errors: 0.

*(Clarify: with “only hash sizes that appear 2+ times in files”, we hash A,B and C,D (two sizes, each appearing twice). So hashed = 4, read = 4, reused = 0.)*

---

## 2. Second scan without any changes

**Setup**
- Same as scenario 1; first scan already completed (all hashed).
- No file content or metadata changed; same 5 files.

**Actions**
- Run second scan on the same folder.
- Run hash phase.

**Assert – grouping**
- Same as after first scan: 2 groups, same membership (A,B and C,D), same reclaimable.
- Summary unchanged.

**Assert – scan 2 stats**
- Files scanned: 5.
- Files with hash: 4 (only duplicate-size candidates get hashed; E has unique size so never hashed).
- Reused: 4 (A, B, C, D were already done and reused via path+size).
- Read from disk: 0.
- Hash errors: 0.

**Assert – scan 1 stats**
- Unchanged (still 5 scanned, 4 with hash, 0 reused, 4 read).

---

## 3. One duplicate file changes (same size)

**Setup**
- After scenario 1: A, B identical; C, D identical; E unique.
- Change content of B so it differs from A but keep the same size (e.g. same length, different bytes).

**Actions**
- Run second scan.
- Run hash phase.

**Assert – grouping**
- B’s content changed (same size), so B must be re-read and re-hashed. A and B then have different hashes → one group (C,D) only.
- Summary: 1 duplicate group (C,D), 2 files, reclaimable = one file size (sizeCD).

**Assert – scan 2 stats**
- Files scanned: 5.
- Files with hash: 4 (only duplicate-size candidates; E never hashed).
- Reused: 3 (A, C, D unchanged; B was queued and re-read).
- Read from disk: 1 (B).
- Hash errors: 0.

---

## 4. One duplicate file changes (different size, unique)

**Setup**
- After scenario 1: A, B identical; C, D identical; E unique.
- Change B so it has different content and a **new size that is unique** (different from A, C, D, and E). So no other file has B's new size.

**Actions**
- Run second scan.
- Run hash phase.

**Assert – grouping**
- A is alone in its “group” (or not in any multi-file group). B has new size; if new size is unique, B is not in any duplicate group. C,D still form one group.
- Summary: 1 duplicate group (C,D), 2 files, reclaimable = one file size.

**Assert – scan 2 stats**
- Files scanned: 5.
- Files with hash: 3 (only duplicate-size candidates; size 9 appears 3× so we queue A, C, D; B and E unique size not hashed).
- Reused: 3 (A, C, D reused from scan 1).
- Read from disk: 0. Hash errors: 0.

**Assert – DB state for changed file**
- B's row must have **hash IS NULL**. We only hash duplicate-size candidates; B's new size (1) is unique, so B was never queued. When we set B to pending (mtime changed), we clear B's hash so the DB does not show an outdated hash.

**Canonical expectations for scenario 4 (B's new size unique)**
- Files with hash: 3 (A, C, D). Reused: 3. Read from disk: 0. Hash errors: 0.

---

## 5. One file changes to match another (becomes duplicate)

**Setup**
- After scenario 1: A, B identical; C, D identical; E unique.
- Change E so its content (and therefore hash) becomes identical to A (and B). Size may change to match A.

**Actions**
- Run second scan.
- Run hash phase.

**Assert – grouping**
- One group of 3 files: A, B, E (same hash). Other group: C, D.
- Summary: 2 groups; one with 3 files, one with 2; reclaimable = size of one copy for each group (e.g. 2 + 1 in “file units”).

**Assert – scan 2 stats**
- Files scanned: 5.
- Files with hash: 5.
- Reused: 4 (A, B, C, D unchanged). E was queued and read (content changed).
- Read from disk: 1 (E).
- Hash errors: 0.

---

## 6. Deleting files between scans

**Setup**
- After scenario 1: A, B, C, D, E.
- Delete B and D from disk (or remove from folder so second scan doesn’t see them).

**Actions**
- Run second scan (only A, C, E present).
- Run hash phase.
- Optionally run “deleted_at” update if that’s part of the flow.

**Assert – grouping**
- Only one duplicate group if we still consider “all files in DB” or only “non-deleted” files. Typically we show duplicate groups for files where `deleted_at IS NULL`. So: A and C are each “alone” (their duplicate was deleted); E was unique. So 0 duplicate groups with more than one file, or we still have two groups but each with 1 file (A; C) and we might hide single-file groups. Plan: 0 duplicate groups (no two files share a hash among non-deleted).
- Summary: 0 groups (or summary reflects only non-deleted files).

**Assert – scan 2 stats**
- Files scanned: 3 (A, C, E).
- Files with hash: 2 (only duplicate-size candidates; A and C hashed/reused, E unique size not hashed).
- Reused: 2 (A, C).
- Read from disk: 0.
- Hash errors: 0.

**Assert – deleted_at / visibility**
- Files B and D have `deleted_at` set (or equivalent) so they don’t appear in “current” duplicate list and don’t affect summary for “current” state.

---

## Test structure (suggested)

- **Fixture**: helper that creates a temp dir and populates it with named files and given content (so we can control duplicates by content). Helper to run scan + hash for a folder and return scan ID(s) and any errors.
- **Assertions**: helpers that take DB + scan ID and assert scan row (file_count, hashed_file_count, hash_reused_count, etc.); helpers that query duplicate_groups_hash and FilesInHashGroup / list by hash and assert group count, file counts per group, reclaimable.
- **Scenarios**: one test per scenario above (or a table-driven test with scenario name, setup function, actions, and expected struct). Each scenario:
  1. Sets up directory and optionally runs first scan.
  2. Applies change (no change / edit file / delete file).
  3. Runs second scan (if applicable).
  4. Asserts grouping (summary + per-group membership/sizes).
  5. Asserts scan 1 stats (and scan 2 stats when applicable).

**Ordering**
- Run scenarios in isolation (clean temp dir and DB state per scenario) so we don’t depend on order. Use a fresh folder and fresh scan(s) per test.

**DB**
- Use Postgres (e.g. TestPostgresDB(t)) so we match production; same schema and logic as real app.

---

## Two-folder scenarios

### 7. Scan folder 1, then folder 2 – cross-folder and within-folder duplicates

**Setup**
- **Folder 1**: Two files with same content (e.g. A1, B1 both "contentAB", size 9). Scan folder 1 and run hash phase.
- **Folder 2**: Files such that:
  - **Same size, different content**: Same size as folder 1 (9 bytes) but different content from folder 1 and from each other where needed.
  - **Same size, equal within folder 2**: Two files in folder 2 with identical content (e.g. C2, D2 both "contentCD").
  - **Same size, equal to folder 1**: One file in folder 2 with the same content as a file in folder 1 (e.g. E2 "contentAB"). This file was **not** scanned in folder 1 (it lives only in folder 2).
  - Optionally: same size, unique content in folder 2 (e.g. F2) so we see a file that is hashed but in no duplicate group.

**Constraint**
- A file in folder 2 that is “equal size to folder one” (and equal content) must not have been scanned on folder 1 – i.e. it is a distinct file in folder 2 that happens to match folder 1 by content.

**Actions**
- Scan folder 1, run hash phase.
- Scan folder 2, run hash phase.

**Assert – grouping**
- One group spans folders: (A1, B1, E2) – same hash.
- One group within folder 2: (C2, D2) – same hash.
- Summary: 2 groups, 5 files in groups. Reclaimable = (3-1)*9 + (2-1)*9 = 18 + 9 = 27 (two copies from 3-file group, one from 2-file group).

**Assert – scan stats**
- Scan 1 (folder 1): 2 scanned, 2 hashed, 0 reused, 2 read.
- Scan 2 (folder 2): 4 scanned (e.g. C2, D2, E2, F2), 4 hashed, 0 reused (different folder/paths), 4 read.

---

### 8. Two folders: first scan zero duplicates (different sizes), second has match and same-size-different

**Setup**
- **Folder 1**: Two files with **different sizes** so no size appears twice → no files hashed (e.g. P1 "a" 1 byte, Q1 "bb" 2 bytes). Scan folder 1 and run hash phase.
- **Folder 2**: Two files equal to P1's content and size (R2, R2b "a") so they get hashed and form one duplicate group; one file (S2 "cc") same size as Q1 but different content (S2 is the only file of that size in scan 2 so not hashed).

**Constraint**
- First scan: zero hashed (different sizes), zero duplicate groups.
- Second scan: one duplicate group (R2, R2b) with same content as P1; S2 matches Q1 in size but not content and is not hashed.

**Actions**
- Scan folder 1, run hash phase.
- Scan folder 2, run hash phase.

**Assert – grouping**
- One group: (P1, R2, R2b) – same hash (cross-folder: P1 from scan 1 is hashed when scan 2’s phase runs via global duplicate-size queue). Reclaimable = 2.
- Q1 and S2 are hashed but different content so not in a group together.

**Assert – scan stats**
- Scan 1 (folder 1): 2 scanned, 2 hashed, 0 reused, 2 read (scan 1 files are hashed during scan 2’s hash phase because their size becomes duplicate globally).
- Scan 2 (folder 2): 3 scanned, 3 hashed, 0 reused, 3 read.

---

## Open points

- **Scenario 4** – Canonical: B's new size is unique, so we never hash B; assert B's hash IS NULL (requires clearing hash when setting file to pending on mtime change).
- **Single-file “groups”** – Whether we expose groups of size 1 in API/UI; tests should align with that.
- **Progress mid-run** – If we want to assert progress updates during hash phase, we’d need to poll scan row or mock time (optional for v1).
- **Idempotency** – Second run of same scan (e.g. “continue” after partial run) could be an extra scenario later.
