# ADR-009: PostgreSQL and new data model (Release 0.2)

**Date**: 2026-02-10

## Decision

1. **Use PostgreSQL for all persistent application data**
   - Replace SQLite (ADR-002, superseded). Connection via a single URL (e.g. `DATABASE_URL` or `DITTO_DATABASE_URL`). The app requires PostgreSQL to run; no embedded DB.

2. **Adopt a normalized data model**
   - **folders** — One row per root path that is set to be scanned. Columns: `id`, `path`, `created_at`.
   - **files** — One row per file location: (folder, path). Columns: `id`, `folder_id`, `path` (relative to folder root), `size`, `mtime`, `inode`, `device_id`, `hash`, `hash_status`, `hashed_at`. Unique `(folder_id, path)`. No `scan_id`; presence in a scan is recorded in the ledger.
   - **scans** — One row per scan run. Columns: `id`, `folder_id`, `started_at`, `completed_at`, `hash_started_at`, `hash_completed_at`, `file_count`, `hashed_file_count`, `hashed_byte_count`, etc.
   - **file_scan** — Ledger: which file was seen in which scan. Columns: `file_id`, `scan_id`. Optionally `removed_in_scan_id` (nullable) for a future “file removed as of this scan” feature. One row per (file_id, scan_id) when the file was present in that scan.

3. **Scan and hash behaviour**
   - Scan phase: For each path under the scanned folder, upsert `files` by `(folder_id, path)` (path relative to folder), then insert `file_scan(file_id, scan_id)`. One row per file per location across scans; ledger links files to scans.
   - Hash phase: “Files to hash for this scan” = files that have a `file_scan` row for this scan and `hash_status = 'pending'` and size in a same-size group within that scan. Hash is stored once per file on `files`; reuse by (inode, device_id) as before.
   - Duplicates across all folders: Single query on `files` (e.g. `GROUP BY hash HAVING COUNT(*) > 1`); no merge across databases.

4. **Configuration**
   - `DATABASE_URL` (or `DITTO_DATABASE_URL`): required for the app to start. Example: `postgres://user:pass@host:5432/dbname?sslmode=disable`.
   - `DITTO_DATA_DIR`: optional; used for non-DB data if needed (e.g. temp files). Not used for the database.

## Context

SQLite’s single-writer model and query performance were limiting (ADR-002). We evaluated alternatives (see [alternatives-to-sqlite.md](alternatives-to-sqlite.md)) and chose PostgreSQL for a single global store with good concurrency. The new schema gives one row per file per location so that “duplicates across all folders” is a single query and the ledger supports future “file removed in later scan” without changing the file row.

## Consequences

- **Positive**
  - No single-writer bottleneck; hash workers and UI can read/write concurrently.
  - One store for all files; duplicate detection across folders is one query.
  - Ledger supports history and future “removed” semantics.
- **Negative**
  - Requires PostgreSQL (second container or managed DB); no single-binary, zero-dependency deployment.
  - Deployment and docs must describe how to run or connect to Postgres (e.g. docker-compose.dev.yml for local dev; production compose or external DB).
- **Neutral**
  - ADR-007 (absolute paths, scan as source of freshness) is implemented via the ledger and relative path under folder; “deletion” can be recorded on the ledger in a later phase.
