# Release 2, Phase 1: PostgreSQL and new data model

**Goal:** Replace SQLite with PostgreSQL and adopt a data model where there is one row per file (per location), a ledger linking files to scans, and clear tables for folders and scans. This removes single-writer contention, enables duplicates across all folders from one store, and sets up for “file removed in later scan” (future).

**Context:** SQLite’s concurrency and query performance are limiting. We are pivoting to PostgreSQL and a normalized model: **folders** (roots to scan), **files** (one row per path per root), **scans** (one row per scan run), and **file_scan** ledger (which file was seen in which scan; supports “removed in scan” later).

**Conflicting decisions:** This phase supersedes or updates:
- **ADR-002 (SQLite):** Superseded. Storage is now PostgreSQL; data directory or env still configurable for non-DB data; connection string (e.g. `DATABASE_URL`) for the DB.
- **ADR-007 (Absolute paths, scan snapshot):** Partially superseded. We keep “scan as source of freshness and deletion” but implement it via the ledger: one row per file (root_id, path with path relative to root); ledger records which scan saw which file; “removed” can be recorded on the ledger (e.g. `removed_in_scan_id`) in a future phase.

**References:** [alternatives-to-sqlite.md](../decisions/alternatives-to-sqlite.md). ADR-005 (scan/hash pipeline), ADR-006 (hashing, hardlinks) unchanged; only storage and schema change.

---

## Data model (agreed)

| Table       | Purpose |
|------------|---------|
| **folders** | Roots that are set to be scanned. Columns: `id`, `path`, `created_at`. |
| **files**   | One row per file location: (folder, path). Columns: `id`, `folder_id`, `path` (relative to folder), `size`, `mtime`, `inode`, `device_id`, `hash`, `hash_status`, `hashed_at`. Unique `(folder_id, path)`. |
| **scans**   | One row per scan run. Columns: `id`, `folder_id`, `started_at`, `completed_at`, `hash_started_at`, `hash_completed_at`, `file_count`, `hashed_file_count`, `hashed_byte_count`, etc. |
| **file_scan** | Ledger: which file was seen in which scan. Columns: `file_id`, `scan_id`, and optionally `removed_in_scan_id` (nullable, for future “deleted as of this scan”). One row per (file_id, scan_id) when the file was present in that scan. |

- **Scan phase:** For each path under the scanned folder, upsert `files` by `(folder_id, path)`, then insert `file_scan(file_id, scan_id)`.
- **Hash phase:** “Files to hash for this scan” = files that have a `file_scan` row for this scan and `hash_status = 'pending'`. Update `files.hash` / `files.hash_status` once per file. Reuse by (inode, device_id) as today.
- **Duplicates across all folders:** Single query on `files` (e.g. `GROUP BY hash HAVING COUNT(*) > 1`), no merge across DBs.

---

## Step 1: Local development environment (Docker Compose + Postgres)

**What:** Provide a local Docker Compose setup that runs PostgreSQL so developers can run Ditto locally (e.g. `go run ./cmd/ditto`) with the app connecting to the containerized database. No need to install Postgres on the host.

**Details:**
- Add a compose file (e.g. `docker-compose.dev.yml` or `compose.dev.yml`) that defines a **postgres** service: fixed port (e.g. 5432), database name, user, and password suitable for development.
- Optionally: a `.env.example` or short doc (e.g. in README or `docs/development.md`) with the default `DATABASE_URL` so developers can `export DATABASE_URL=...` and run the app.
- Developer workflow: `docker compose -f docker-compose.dev.yml up -d` (or `docker compose up -d` if using a single dev compose), then run the app with `DATABASE_URL` set. No Ditto container in this step unless we later add “run full stack in compose” for dev.

**Deliverables:**
- `docker-compose.dev.yml` (or equivalent) at repo root with a postgres service (image, port 5432, env for POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB). Use a volume for data so the DB persists between runs.
- README or `docs/development.md`: one or two sentences on how to start Postgres for local dev and set `DATABASE_URL` to connect the app.

**Review:** Running the compose brings up Postgres; connecting with the documented URL works (e.g. `psql` or the app once Step 3 and 4 are in place).

---

## TDD and review

- Migrations: PostgreSQL migrations create the four tables and indexes; idempotent or versioned migration runner.
- DB layer: Tests against a real Postgres (e.g. testcontainer or CI Postgres) or in-memory equivalent; no SQLite in new code paths.
- Scan phase: Unit or integration test that after a scan, files are upserted by (folder_id, path) and file_scan rows link them to the scan.
- Hash phase: Same as today (candidates, priority, workers) but against new schema; hash stored on `files`; reuse by inode still works.
- UI/API: List scans, list folders, duplicate groups and file list from `files` (+ ledger where needed); no regression.

---

## Step 2: ADR and config for PostgreSQL

**What:** Document the decision to use PostgreSQL and the new data model (new ADR or “Release 0.2 pivot” decision). Add configuration for database connection (e.g. `DATABASE_URL` or `DITTO_DATABASE_URL`); keep `DITTO_DATA_DIR` for any non-DB data if needed.

**Deliverables:**
- New ADR (e.g. ADR-009) or update to docs: PostgreSQL as primary store; new schema (folders, files, scans, file_scan). Supersedes ADR-002; ADR-007 updated for ledger-based freshness/deletion.
- Config: `DATABASE_URL` (or equivalent) in env; document required env for Docker and Synology (e.g. second container or managed Postgres).

**Review:** ADR and config documented; app can read DB URL from env.

---

## Step 3: PostgreSQL migrations and schema

**What:** Introduce a migration runner and create migrations for PostgreSQL: `folders`, `files`, `scans`, `file_scan` with indexes (e.g. `files(folder_id, path)` unique; `files(hash)` for duplicate groups; `files(inode, device_id)` for reuse; `file_scan(scan_id, file_id)`; `scans(folder_id)`).

**Deliverables:**
- Migration tooling (e.g. embed SQL files, run on startup or via CLI).
- Initial migration: CREATE TABLEs and indexes; no data migration from SQLite yet (fresh start for 0.2 or separate step for one-time import).

**Review:** Migrations run cleanly on a fresh Postgres; schema matches the agreed model.

---

## Step 4: Port DB layer to PostgreSQL

**What:** Replace SQLite-specific code with PostgreSQL-compatible SQL. New packages or refactored `internal/db`: open by `DATABASE_URL`, use `lib/pq` or `pgx`; all queries use Postgres dialect (e.g. `$1` placeholders if using pgx, or keep `?` and use a driver that supports it). Remove SQLite driver, WAL, busy timeout, and read-only connection pool (Postgres handles concurrent readers and writers).

**Deliverables:**
- `internal/db` (or equivalent) talks only to PostgreSQL.
- Scans: ListScans, GetScan, CreateScan (with folder_id), UpdateScanCompletedAt, etc.
- Files: UpsertFile by (folder_id, path), GetFile, UpdateFileHash, ListFilesForHashGroup, etc.
- Ledger: InsertFileScan, optionally ListFilesForScan (files that have file_scan for this scan).
- Duplicates: DuplicateGroupsByHash (across all or filtered by scan/folder), FilesInHashGroup, etc., all from `files` (and file_scan when scoping to a scan).

**Review:** All existing call sites (scan phase, hash phase, server handlers) use the new layer; tests pass against Postgres.

---

## Step 5: Port scan phase to new schema

**What:** Scan phase walks the tree as today; for each file, upsert into `files` by `(folder_id, path)` (path relative to folder root), then insert `file_scan(file_id, scan_id)`. Create scan row with `folder_id` at start; update scan row (file count, completed_at) at end. Remove any SQLite-specific scan logic.

**Deliverables:**
- Scan phase writes to PostgreSQL only; one row per file per location; ledger links file to scan.
- Tests: after scan, file count in `files` and `file_scan` matches expectations; rescan updates existing file rows and adds new ledger rows.

**Review:** Scan phase runs end-to-end against Postgres; no SQLite references.

---

## Step 6: Port hash phase to new schema

**What:** Hash phase: “candidates” = files that have a `file_scan` row for this scan and `hash_status = 'pending'` and size in a same-size group (within this scan). Priority and worker pool unchanged. On completion, update `files.hash`, `files.hash_status`, `files.hashed_at`; update scan row (hashed_file_count, hashed_byte_count, hash_completed_at). Reuse: look up file by (inode, device_id) with non-null hash (same or previous scan) and assign that hash to the current file.

**Deliverables:**
- Hash phase runs against PostgreSQL; duplicate detection and reuse logic preserved.
- Tests: hash phase completes; files have hashes; duplicate groups query returns correct groups across folders.

**Review:** Hash phase and duplicate queries work with new schema.

---

## Step 7: Port server and UI to new schema

**What:** HTTP server and UI use the new DB layer: list folders (from `folders`), list scans (from `scans`), duplicate groups and file lists from `files` (and `file_scan` when “for this scan”). Remove read-only pool and SQLite-specific handling. Config: require `DATABASE_URL` (or fail fast with clear message).

**Deliverables:**
- All handlers use PostgreSQL; “duplicates across all folders” is a single query on `files`.
- Docker/docs: document that Ditto 0.2 requires PostgreSQL (e.g. second container or external DB); update docker-compose example and Synology docs.

**Review:** UI shows folders, scans, and duplicate groups; no SQLite; deployment docs updated.

---

## Step 8: Remove SQLite and finalize

**What:** Remove SQLite driver, migrations, and any `DITTO_DATA_DIR`-for-DB usage (keep data dir only if used for other data). Clean up ADR-002 reference (marked superseded). Ensure release notes and README describe PostgreSQL as a requirement.

**Deliverables:**
- Codebase has no SQLite dependency for main app (tests may still use SQLite for legacy tests or remove them).
- README and release docs state PostgreSQL requirement and link to setup.

**Review:** Build and tests pass; docs accurate.
