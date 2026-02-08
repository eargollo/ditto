# Ditto implementation plan

This folder holds the phased implementation plan for Ditto. The plan is driven by the [architecture decisions](../decisions/README.md) (ADRs).

## How we work

- **Phases** are high-level milestones. Each phase has a dedicated markdown file (e.g. `phase-1-foundation.md`) with a **detailed plan**.
- The detailed plan breaks the phase into **small steps** that are suitable for incremental implementation and **code review** (one or a few PRs per step).
- We implement in a **TDD fashion**: tests first (or test-and-implement in small slices), then code.
- Phase documents are written one at a time. Phase 1 will be written after this master plan is reviewed.

---

## High-level phases

| Phase | Name | Goal |
|-------|------|------|
| **1** | Foundation | Go module, project layout, SQLite connection and schema (scans, files with path/size/mtime/inode, hash status), config (data path and port from env). No UI yet; app can open DB and run migrations. |
| **2** | Scan | Walk the file tree, collect metadata (path, size, mtime, inode), skip symlinks, apply path excludes. Write results to SQLite. Optional scan throttling (concurrency/rate). |
| **3** | Hash pipeline | Build the hash queue (same-size candidates only), priority (size or size×count), bounded worker pool, SHA-256 hashing. Reuse known hash for hardlinks (same inode). Throttle hashing. |
| **4** | Web UI shell | HTTP server, Tailwind build, HTMX, base layout and static assets. Placeholder pages. No auth (v1). Configurable port. |
| **5** | Web UI – Scans and results | Configure scan roots, start scan, show progress (e.g. HTMX polling). List duplicate groups (by hash and by inode). View group details and file list. |
| **6** | Actions and delivery | User actions on duplicates (e.g. delete selected, keep one). Excludes and throttle settings in the UI. Docker image, docker-compose example, and usage docs for Synology. |
| **7** | Recovery from failure | Robust recovery when the process or system fails during hash (or scan) phase: e.g. reset orphaned `hashing` to `pending`, optional lease/timeout or run-id so hash phase can complete after restart. |

---

## Phase documents

| Phase | Document | Status |
|-------|----------|--------|
| 1 | [phase-1-foundation.md](phase-1-foundation.md) | Done |
| 2 | [phase-2-scan.md](phase-2-scan.md) | Done |
| 3 | [phase-3-hash-pipeline.md](phase-3-hash-pipeline.md) | Ready |
| 4 | `phase-4-web-ui-shell.md` | Not started |
| 5 | `phase-5-web-ui-scans-and-results.md` | Not started |
| 6 | `phase-6-actions-and-delivery.md` | Not started |
| 7 | `phase-7-recovery-from-failure.md` | Not started |
