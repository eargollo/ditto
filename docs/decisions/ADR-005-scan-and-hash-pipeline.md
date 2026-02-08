# ADR-005: Separate scan from hashing with prioritized hash queue

**Date**: 2025-02-07

## Decision

1. **Separate file scanning from content hashing**
   - **Scan phase:** Walk the configured directory tree and collect metadata only: path, size, mtime, and inode. Write these records into SQLite (e.g. a `files` table keyed by scan). No file content is read during the scan.
   - **Hash phase:** Run after (or alongside) the scan, and only for files that are duplicate candidates: i.e. files whose size equals at least one other file in the same scan. Files with a unique size are never hashed, since they cannot be duplicates.

2. **Store inode in file metadata**
   - Persist inode (and device id if needed) so we can detect when a file has moved (same inode, different path). This allows updating paths or skipping re-hash for moved files and keeps duplicate groups correct across renames/moves.

3. **Hash work as a priority queue in SQLite**
   - Hash jobs are represented in the database (e.g. a `hash_queue` or derived from `files` plus a “hash status” column). Only files in size groups with more than one file are queued for hashing.
   - **Priority:** Order hash work by a configurable strategy. Options we support: by **size** (e.g. largest first to find big duplicates sooner), or by **size × count** (total bytes in that size group — prioritize groups that represent more duplicate potential or more files to dedupe). Larger priority value = process first.
   - A **bounded worker pool** drains the queue: a fixed number of workers (configurable, with a sensible default e.g. 4) take jobs from the queue in priority order, hash the file, and update the record. This limits concurrent file reads and CPU use without opening hundreds of files at once.

4. **Throttle hashing**
   - Limit hashing to a configurable rate (e.g. N files per minute, or max bytes read per second) so the NAS I/O and CPU are not saturated.

5. **Throttle scanning (optional, lighter than hashing)**
   - Support optional throttling for the scan phase so the NAS stays responsive during the tree walk. Options: cap **concurrent walkers** if we parallelize the scan (e.g. one or a few walkers), and/or a configurable **rate limit** (e.g. max files or dirs per second). Defaults should be off or very permissive so normal scans stay fast; users can tighten them if the NAS is busy or the tree is huge.

## Context

Scanning large trees and hashing every file would be slow and wasteful. Duplicates must have the same size, so we only need to hash files that share their size with at least one other file. Separating scan from hashing also lets users see “scan done, now hashing N candidates” and pause/resume or throttle hashing independently. Storing the hash queue in SQLite (ADR-002) keeps it persistent, queryable, and resumable. Prioritizing by size or by size×count focuses effort where duplicate cleanup yields the most benefit (large files or large groups). Inode storage improves correctness when users move or rename files between scans. Throttling both phases keeps the NAS responsive; scan throttle is lighter than hash throttle so the scan remains fast by default.

## Consequences

- **Positive**
  - No hashing for unique-size files; large savings in I/O and CPU on typical trees.
  - Clear two-phase model: fast metadata scan, then bounded hash workload with priority and throttle.
  - Worker pool caps concurrency so I/O and CPU load are predictable and the NAS stays responsive.
  - Optional scan throttling (concurrency cap and/or rate limit) lets users tune metadata load on busy or large trees.
  - Queue in SQLite is durable, visible, and resumable across restarts.
  - Inode-based detection of moved files keeps data consistent and can avoid redundant work.
- **Negative**
  - Schema and logic must support queue state (pending/hashing/done/failed), priority computation, and inode (and device) handling; inodes are filesystem-specific and not meaningful across devices.
  - Priority and throttle knobs (scan and hash) need defaults and possibly UI (or config) so users can tune.
- **Neutral**
  - Scan throttle defaults can be “off” or very permissive so we don’t slow down typical scans; power users enable it when needed.
