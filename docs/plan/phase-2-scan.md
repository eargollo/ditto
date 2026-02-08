# Phase 2: Scan

**Goal:** Walk the file tree, collect metadata (path, size, mtime, inode, device_id), skip symlinks, apply path excludes. Write results to SQLite. Optional scan throttling.

**References:** [ADR-005](../decisions/ADR-005-scan-and-hash-pipeline.md) (scan phase, inode, throttle), [ADR-006](../decisions/ADR-006-hashing-and-duplicate-definition.md) (skip symlinks).

---

## TDD and review

- Each step is implemented **test-first**: write the test(s), see them fail, then implement until they pass.
- One step = one logical change set. Prefer one PR per step.
- **Review checklist** per step: tests exist and pass; code is minimal; naming and layout match the plan.

---

## Step 1: File walk (metadata only, skip symlinks)

**What:** A walker that traverses a root directory and yields metadata for each **regular file** only. Use `os.Lstat` (do not follow symlinks). Yield path (absolute or relative to root), size, mtime, inode, device_id. Skip directories (we don’t store them in `files`). Skip symlinks entirely (ADR-006: do not treat as part of the tree).

**TDD:**
- Test: create a temp dir with a few regular files and at least one symlink; walk and collect entries; assert only regular files are yielded, with correct path/size/mtime/inode; symlink is not yielded.
- Test: walk an empty dir yields nothing.
- Test: nested dirs: walk yields only regular files at any depth.

**Deliverables:**
- Package `internal/scan` (or similar): a function that walks a root and calls a callback for each regular file, e.g. `Walk(ctx, root string, fn func(Entry) error) error`, or a channel/iterator. `Entry` has Path, Size, MTime, Inode, DeviceID.
- Use `filepath.WalkDir` with `fs.DirEntry` and `Lstat` so symlinks are not followed. For each entry: if regular file, yield; if symlink or dir, skip (don’t recurse into symlinks).

**Review:** Symlinks never yielded; only regular files; inode and device_id come from `Sys()` (syscall) or equivalent; tests use temp dirs.

---

## Step 2: Path excludes

**What:** Accept a list of exclude patterns (e.g. glob or path segments) and skip any file or directory whose path matches. Examples: `.git`, `node_modules`, `*.tmp`. During walk, before yielding or recursing, check path against excludes.

**TDD:**
- Test: walk a temp dir that contains a path matching an exclude (e.g. `foo/.git/bar` or `a/b/node_modules`); assert that file (and optionally entire excluded subtree) is not yielded.
- Test: exclude pattern that matches a file (e.g. `*.log`); assert that file is not yielded.
- Test: no excludes: all regular files still yielded (existing behavior).

**Deliverables:**
- Exclude matcher: e.g. `ShouldExclude(path string, patterns []string) bool`. Pattern format: simple glob (`*.tmp`) or path segment (`.git` = any path containing `/.git` or component `.git`). Keep rules simple for v1 (e.g. `filepath.Match` + segment check).
- Walker calls the matcher for each path (and for dirs before recursing); skip if excluded.

**Review:** Excludes are applied consistently; tests cover segment and glob; default (nil/empty patterns) means no exclusions.

---

## Step 3: Scan runner (wire walk to DB)

**What:** Given a root path and optional excludes: create a scan row, walk the tree, insert a file row for each yielded entry, then set the scan’s `completed_at`. All under one transaction or with explicit error handling so we don’t leave scans without `completed_at` on partial failure.

**TDD:**
- Test: run scan against a temp dir with a few known files; assert one scan row with non-null `completed_at`, and N file rows with correct scan_id and paths.
- Test: run scan with exclude that hides some files; assert file count is reduced and excluded paths are not in DB.
- Test: run scan on non-existent root; assert error and no scan row (or scan row with error state, depending on design).

**Deliverables:**
- `RunScan(ctx, db, rootPath string, opts ScanOptions) (scanID int64, err error)` or similar. `ScanOptions` holds ExcludePatterns (and later throttle settings).
- Use `db.CreateScan`, then walk with excludes, then `db.InsertFile` for each entry. On success, set `scans.completed_at` (add `db.UpdateScanCompletedAt(ctx, db, scanID)` or equivalent in Phase 1 db package if not yet present).
- On walk or insert error: either roll back / delete scan, or set completed_at and return error; document behavior.

**Review:** Scan and files are consistent; completed_at set on success; tests use in-memory or temp DB.

---

## Step 4: Optional scan throttle (files per second)

**What:** Support optional throttling for the scan phase (ADR-005) by limiting files yielded per second so the NAS stays responsive. Use **`golang.org/x/time/rate`**: when `MaxFilesPerSecond > 0`, call `limiter.Wait(ctx)` before each `fn(e)` so we don’t exceed the rate; when **0**, no throttle and we go full speed.

**TDD:**
- Test: with throttle enabled (e.g. 10 files/s), walk a dir with 3 files; assert elapsed time is at least ~200ms (two delays after first file). Use generous margins so tests don’t flake.
- Test: with throttle disabled (`MaxFilesPerSecond == 0`), walk is fast (no artificial delay).

**Deliverables:**
- Add dependency: `golang.org/x/time/rate`.
- `ScanOptions` gains `MaxFilesPerSecond int` (0 = no throttle, full speed). In `RunScan`, pass it into `Walk`. In the walk loop, when `MaxFilesPerSecond > 0`, create `rate.NewLimiter(rate.Limit(maxFilesPerSecond), 1)` (burst 1 for smooth rate) and before each `fn(e)` call `limiter.Wait(ctx)`; when 0, skip the limiter so there is no overhead.
- Optional later: cap on concurrent walkers if we parallelize; for now, rate limit is the only lever.

**Review:** Default 0 means full speed; rate limit is applied only when set; tests use generous timing margins.

---

## Phase 2 done when

- [ ] All steps implemented with tests first (TDD).
- [ ] `go build ./...` and `go test ./...` pass.
- [ ] Reviewer has approved each step (or each PR).
- [ ] Running a scan (via API or CLI added in a later phase) against a directory populates the DB with one scan and N file rows, and the scan has `completed_at` set.
