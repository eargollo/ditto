# Phase 5: Web UI – Scans and results

**Goal:** Configure scan roots, start scan, show progress (e.g. HTMX polling). List duplicate groups (by hash and by inode). View group details and file list.

**References:** [ADR-003](../decisions/ADR-003-web-ui.md), [ADR-004](../decisions/ADR-004-htmx-tailwind.md), [ADR-007](../decisions/ADR-007-absolute-paths-and-scan-freshness.md) (queries scoped to scan_id).

---

## TDD and review

- Handlers and DB helpers: test-first where practical; UI flows can be verified manually or with a few integration tests.
- One step = one logical change set.

---

## Step 1: Scan roots storage and API

**What:** Persist "scan roots" (paths to scan) so the UI can list them and start a scan for a root. Add a table and CRUD-style handlers (list, add, optional delete). No UI form yet if you prefer to do "list roots" and "add root" in Step 2.

**TDD:**
- Test: insert a scan root, list roots returns it; add another, list returns two (e.g. order by id or path).
- Test: handler GET /api/roots (or /scans/roots) returns JSON or HTML fragment with roots. Optional: POST /api/roots with path adds a root.

**Deliverables:**
- Migration: table `scan_roots` with `id`, `path` (TEXT NOT NULL), `created_at` (TEXT). Unique or not on path: allow same path twice or constrain; for v1 allow duplicates and let user delete later.
- `internal/db`: `ListScanRoots(ctx, db) ([]ScanRoot, error)`, `AddScanRoot(ctx, db, path string) error`, optional `DeleteScanRoot(ctx, db, id int64) error`. `ScanRoot` struct: ID, Path, CreatedAt.
- Handlers: GET /scans/roots (or /api/roots) returns list; POST /scans/roots (form or JSON) adds a root and redirects or returns 201.

**Review:** Roots are stored and listed; migration is idempotent.

---

## Step 2: Scans list and start-scan flow

**What:** Page that lists scan roots and recent scans. "Start scan" for a root: POST to start a new scan (run scan + hash in background or synchronously for v1; if background, return immediately with scan_id). Redirect to a "scan progress" page for that scan_id.

**TDD:**
- Test: POST /scans/start with root_id or path creates a scan (and optionally starts hash phase); response is redirect to /scans/{id} or 200 with scan_id.
- Test: GET /scans lists recent scans (from db.ListScans) and roots (from db.ListScanRoots).

**Deliverables:**
- Page GET /scans: show table of recent scans (id, root_path, created_at, completed_at, hash status/metrics) and list of roots with "Start scan" button per root. Use layout and Tailwind.
- POST /scans/start: body or form with `root_path` (or root_id). Create scan via scan.RunScan (and hash.RunHashPhase) — for v1 run in foreground so the request blocks until done; or run in a goroutine and return redirect to /scans/{id} so user can poll progress. Prefer background goroutine so UI stays responsive.
- Link "Scan" in nav goes to /scans.

**Review:** User can start a scan from the UI; scans list shows recent scans; start-scan does not block forever if we use background (recommended).

---

## Step 3: Scan progress page with HTMX polling

**What:** Page for a single scan (e.g. GET /scans/{id}) that shows scan status: root_path, created_at, completed_at, hash_started_at, hash_completed_at, hashed_file_count, hashed_byte_count. Use HTMX polling (hx-trigger="every 2s" or load every 2s) to refresh a status fragment until the scan is complete (completed_at and hash_completed_at set).

**TDD:**
- Test: GET /scans/123 returns 200 and HTML with scan info; if scan_id invalid, 404.
- Test: GET /scans/123/status (fragment endpoint) returns HTML fragment with status; HTMX can swap it into the page.

**Deliverables:**
- GET /scans/{id}: full page with scan title and a div that loads status fragment via HTMX (hx-get="/scans/{id}/status" hx-trigger="every 2s" hx-swap="innerHTML"). Optional: stop polling when scan and hash are complete (hx-trigger with condition or remove trigger when done).
- GET /scans/{id}/status: returns HTML fragment (table or div) with scan fields; use db.GetScan. If scan not found, return 404 fragment or 404 status.
- Link from scans list to /scans/{id}.

**Review:** Progress page updates every 2s; user sees when scan and hash complete.

---

## Step 4: Duplicate groups list (by hash and by inode)

**What:** For a scan, list "duplicate groups": (1) groups of files that share the same hash (content duplicates); (2) groups of files that share the same inode (hardlinks). Each group shows: hash or inode, count of files, total size (or representative size). Clicking a group goes to group detail (Step 5).

**TDD:**
- Test: DB helper returns groups for a scan. E.g. `DuplicateGroupsByHash(ctx, db, scanID)` returns []Group where each has Hash, FileCount, Size (or paths). Similarly `DuplicateGroupsByInode` or a combined view.
- Test: GET /scans/{id}/duplicates returns 200 and list of groups (or fragment); empty scan returns empty list.

**Deliverables:**
- `internal/db`: `DuplicateGroupsByHash(ctx, db, scanID) ([]DuplicateGroup, error)` where DuplicateGroup has Hash, Count, Size (sum or first file size), and optionally FileIDs or Paths. Query: SELECT hash, COUNT(*), SUM(size) FROM files WHERE scan_id = ? AND hash IS NOT NULL GROUP BY hash HAVING COUNT(*) > 1. Similarly `DuplicateGroupsByInode(ctx, db, scanID)` for inode groups (same inode + device_id).
- Page GET /scans/{id}/duplicates: two sections or tabs — "By content (hash)" and "By inode (hardlinks)". Each lists groups with link to /scans/{id}/duplicates/hash/{hash} or /scans/{id}/duplicates/inode/{inode} (or query param). Use layout.
- Link from scan progress page to "View duplicates" -> /scans/{id}/duplicates.

**Review:** Duplicate groups are computed from DB; UI lists them and links to detail.

---

## Step 5: Group detail – file list

**What:** Page that shows all files in one duplicate group (by hash or by inode). List: path, size, mtime (optional). For "by hash" group: list files with that hash in this scan. For "by inode" group: list files with that (inode, device_id) in this scan. Actions (delete, keep one) are Phase 6; this step is read-only.

**TDD:**
- Test: DB helper `FilesInHashGroup(ctx, db, scanID, hash)` returns files with that hash. `FilesInInodeGroup(ctx, db, scanID, inode, deviceID)` returns files with that inode (and device_id).
- Test: GET /scans/{id}/duplicates/hash/{hash} returns 200 and list of file rows; invalid hash or scan returns 404.

**Deliverables:**
- `internal/db`: `FilesInHashGroup(ctx, db, scanID int64, hash string) ([]File, error)`. `FilesInInodeGroup(ctx, db, scanID int64, inode int64, deviceID *int64) ([]File, error)`.
- GET /scans/{id}/duplicates/hash/{hash}: page with table of files (path, size). Same for inode: GET /scans/{id}/duplicates/inode?inode=123&device_id=1 (or path-safe encoding). Use layout; link back to /scans/{id}/duplicates.

**Review:** User can open a group and see all file paths; read-only; Phase 6 will add actions.

---

## Phase 5 done when

- [x] User can add scan roots and see them on /scans.
- [x] User can start a scan from the UI; scan runs (and hash runs); redirect to progress page.
- [x] Progress page polls and shows scan + hash status until complete.
- [x] Duplicate groups (by hash and by inode) are listed for a scan; user can open a group and see the file list.
- [x] All handlers use layout and Tailwind; no auth (v1).
