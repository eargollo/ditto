# ADR-007: Absolute paths and scan as source of freshness and deletion

**Date**: 2025-02-07  
**Status:** Partially superseded by Release 0.2. We keep “scan as source of freshness and deletion” but implement it via the new data model: one row per file (folder_id, path with path relative to root); ledger (`file_scan`) records which scan saw which file; “removed” can be recorded on the ledger in a future phase. See [rel-2-phase-1-postgres-and-data-model.md](../plan/rel-2-phase-1-postgres-and-data-model.md).

## Decision

1. **Store absolute paths for files**
   - File paths in the `files` table are stored as absolute paths (the full path at scan time). This avoids the “same file, two paths” problem when different scan roots overlap (e.g. scanning a parent and then a child directory). Resolving a path for hashing or display is then trivial: the path is the path to open. When a file is moved on the same device, we can refresh the path on a later rescan by matching (device_id, inode) and updating the path for that row in the new scan’s context; historical scans keep their snapshot as-is.

2. **Each scan is a consistent snapshot; scan_id informs deletion and freshness**
   - Each scan run gets a new `scan_id` and new file rows. We do not update existing rows’ `scan_id` when we rescan. So every row belongs to exactly one scan, and that scan represents “what we saw at that time.”
   - **Deletion:** If a file was present in scan 1 and is not present in scan 2 (same root), we infer it was deleted (or moved out of the tree). We do not need to open the path to discover that; the absence from the latest scan tells us. Rows that exist only in older scans are therefore “no longer in the tree” for current purposes.
   - **Freshness:** The latest scan (by `created_at` or `id`) is the current view of the tree. Older scans are historical snapshots. When we report “what exists,” “what are duplicates,” or “what can be deleted,” we work from a chosen scan—typically the latest.

3. **Duplicate and “current state” queries are scoped to a scan**
   - Queries that drive duplicate detection, space savings, or file actions (e.g. hash, delete) must be scoped to a specific `scan_id`. In practice we will use the **latest** scan (or the one the user selects) so that we only consider files that are still in the tree and avoid stale paths. We do not mix files from multiple scans for a single “current” duplicate report unless we explicitly design a cross-scan feature later.

## Context

We needed to decide whether to store paths as absolute or relative to the scan root, and how to interpret multiple scans over time. Relative paths caused ambiguity when the same directory was scanned under different roots (same file, two path strings). Absolute paths avoid that. The downside of absolute paths is staleness when files move; we mitigate by (1) treating each scan as a snapshot so that “not in latest scan” means “deleted” and (2) optionally refreshing path on rescan via (device_id, inode). Making scan_id the scope for “current” duplicate and file-state queries keeps behavior consistent and avoids acting on files that no longer exist.

## Consequences

- **Positive**
  - Same file always has one canonical path per scan; no double-counting from overlapping roots.
  - Deletion and freshness are inferred from scan membership; we don’t depend on opening paths to know a file is gone.
  - Duplicate logic is clear: “duplicates in scan X” and “latest scan” are well-defined.
- **Negative**
  - Absolute paths break if the volume is renamed or the DB is moved to another machine without a path-rewrite or rescan; we accept that and document it.
- **Neutral**
  - When we implement “latest scan” we can define it as “scan with maximum id (or created_at) for a given root_path” or “most recent scan overall” depending on product needs.
