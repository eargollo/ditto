# ADR-002: SQLite for persistent storage with external data mount

**Date**: 2025-02-07  
**Status:** Superseded by Release 0.2 (PostgreSQL and new data model). See [rel-2-phase-1-postgres-and-data-model.md](../plan/rel-2-phase-1-postgres-and-data-model.md) and [alternatives-to-sqlite.md](alternatives-to-sqlite.md).

## Decision

1. **Use SQLite for all persistent application data**
   - File metadata (path, size, mtime, hashes), scan metadata, duplicate groups, and any user preferences will be stored in a single SQLite database.
   - Use WAL mode for better write throughput during large scans.
   - Access via Go’s `database/sql` with a portable driver (e.g. `modernc.org/sqlite`).

2. **Support an externally mounted data file**
   - The database file path must be configurable (e.g. environment variable or flag), defaulting to a sensible in-container path when not set.
   - When running in Docker, users may mount a host volume (e.g. Synology shared folder) onto that path so the database persists across container restarts and upgrades.
   - Documentation and examples (e.g. `docker-compose.yml`) will show how to mount the data directory so the SQLite file lives on the host.

## Context

Ditto needs to persist scan results, file hashes, and duplicate groups so that users don’t have to rescan from scratch after restarts. We considered SQLite, key-value stores (Bolt, Badger), and a separate RDBMS (PostgreSQL). SQLite was chosen because it requires no separate server, fits the single-process Docker/SPK model, and supports the query patterns we need (by hash, size, path) with standard SQL and indexes.

On Synology and in Docker, container storage is often ephemeral or tied to the container lifecycle. Allowing the SQLite file to live on a mounted volume gives users durable storage, easier backups (e.g. via Synology backup tools), and the ability to upgrade the app image without losing data.

## Consequences

- **Positive**
  - Single-file database is easy to backup, move, or restore.
  - External mount keeps data across container updates and restarts.
  - No database server to run or maintain; the app stays self-contained.
  - Configurable path works for both default (in-container) and volume-mount usage.
- **Negative**
  - We must document the mount pattern and ensure the app handles missing or read-only data paths gracefully.
  - Users must choose and create a host path when they want persistence (e.g. `/volume1/docker/ditto/data`).
- **Neutral**
  - Default path can be something like `/data/ditto.db` or `./data/ditto.db` so that a single volume mount (e.g. `-v /path/on/host:/data`) is sufficient.
