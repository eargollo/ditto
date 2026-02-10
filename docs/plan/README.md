# Ditto implementation plan

This folder holds the phased implementation plan for Ditto. The plan is driven by the [architecture decisions](../decisions/README.md) (ADRs).

## How we work

- **Releases** group phases into shippable milestones (v0.1, v0.2, v0.3).
- **Phases** are high-level milestones within a release. Phases are numbered from 1 in each release. Each phase has a dedicated markdown file (e.g. `rel-1-phase-1-foundation.md`) with a **detailed plan**.
- The detailed plan breaks the phase into **small steps** that are suitable for incremental implementation and **code review** (one or a few PRs per step).
- We implement in a **TDD fashion**: tests first (or test-and-implement in small slices), then code.

---

## Release v0.1

**Goal:** First usable release: scan folders, find duplicates, view results in the UI. Establish a release and release process so the app can be deployed on Synology; then iterate on UX in later minor releases.

| Phase | Name | Goal |
|-------|------|------|
| **1** | Foundation | Go module, project layout, SQLite connection and schema (scans, files with path/size/mtime/inode, hash status), config (data path and port from env). No UI yet; app can open DB and run migrations. |
| **2** | Scan | Walk the file tree, collect metadata (path, size, mtime, inode), skip symlinks, apply path excludes. Write results to SQLite. Optional scan throttling (concurrency/rate). |
| **3** | Hash pipeline | Build the hash queue (same-size candidates only), priority (size or size×count), bounded worker pool, SHA-256 hashing. Reuse known hash for hardlinks (same inode). Throttle hashing. |
| **4** | Web UI shell | HTTP server, Tailwind build, HTMX, base layout and static assets. Placeholder pages. No auth (v1). Configurable port. |
| **5** | Web UI – Scans and results | Configure scan roots, start scan, show progress (e.g. HTMX polling). List duplicate groups (by hash and by inode). View group details and file list. |
| **6** | Synology releasable | Docker image, packaging, release process, and docs so the app can be deployed and used on Synology. Enables testing and iteration. |
| **7** | UX refinement | Polish UI/UX based on usage; small improvements. Can drive multiple minor releases (v0.1.1, v0.1.2, …) once the release process is in place. |

| Phase | Document | Status |
|-------|----------|--------|
| 1 | [rel-1-phase-1-foundation.md](rel-1-phase-1-foundation.md) | Done |
| 2 | [rel-1-phase-2-scan.md](rel-1-phase-2-scan.md) | Done |
| 3 | [rel-1-phase-3-hash-pipeline.md](rel-1-phase-3-hash-pipeline.md) | Done |
| 4 | [rel-1-phase-4-web-ui-shell.md](rel-1-phase-4-web-ui-shell.md) | Done |
| 5 | [rel-1-phase-5-web-ui-scans-and-results.md](rel-1-phase-5-web-ui-scans-and-results.md) | Done |
| 6 | [rel-1-phase-6-synology-releasable.md](rel-1-phase-6-synology-releasable.md) | Done |
| 7 | `rel-1-phase-7-ux-refinement.md` | Not started |

---

## Release v0.2

**Goal:** Scheduled scans and better progress feedback during long runs.

| Phase | Name | Goal |
|-------|------|------|
| **1** | Weekly scheduled scan | User configures weekday + hour (hourly granularity). A goroutine wakes at the top of each hour; when weekday and hour match, enqueue one scan per folder (serialized via existing queue). Manual "Start scan" unchanged. |
| **2** | Progress and feedback | Improve UX during long runs: progress bar for hash phase, "files discovered" during scan, and detail such as the file currently being hashed. |

| Phase | Document | Status |
|-------|----------|--------|
| 1 | [rel-2-phase-1-weekly-schedule.md](rel-2-phase-1-weekly-schedule.md) | Ready |
| 2 | [rel-2-phase-2-progress-and-feedback.md](rel-2-phase-2-progress-and-feedback.md) | Ready |

---

## Release v0.3

**Goal:** User actions on duplicates and robust recovery from failure.

| Phase | Name | Goal |
|-------|------|------|
| **1** | Actions and delivery | User actions on duplicates (e.g. delete selected, keep one). Excludes and throttle settings in the UI. Docker image, docker-compose example, and usage docs for Synology. |
| **2** | Recovery from failure | Robust recovery when the process or system fails during hash (or scan) phase: e.g. reset orphaned `hashing` to `pending`, optional lease/timeout or run-id so hash phase can complete after restart. |

| Phase | Document | Status |
|-------|----------|--------|
| 1 | `rel-3-phase-1-actions-and-delivery.md` | Not started |
| 2 | `rel-3-phase-2-recovery-from-failure.md` | Not started |

---

## Improvements (backlog)

See [improvements.md](improvements.md) for optional follow-ups (e.g. concurrent directory reads for scan performance).
