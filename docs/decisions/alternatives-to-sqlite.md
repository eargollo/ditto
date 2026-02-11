# Alternatives to SQLite for Ditto Storage

**Context**: SQLite is causing concurrency pain (SQLITE_BUSY, retries) and slow queries. We don’t handle huge data volumes and each scan is self-contained. This doc outlines alternatives that fit those constraints.

**Core requirement**: Find duplicate files **across all folders** being scanned — e.g. same content in `/volume1/Photos` and `/volume2/Backup`. The “global” duplicate view (across origins) is the main use case.

---

## Why SQLite hurts here

- **Single writer**: One write connection; hash phase has 6 workers all doing `UPDATE` + reads (HashForInode, HashForInodeFromPreviousScan). Even with WAL and a separate read-only pool, the writer is a bottleneck and BUSY retries add latency.
- **Heavy per-file updates**: Many small transactions during the hash phase amplify lock contention.

---

## Option 1: One database (or file) per scan + small catalog (recommended)

**Idea**: Remove shared-writer contention by giving each scan its own store. Only the catalog is shared (and it’s write-light).

- **Catalog** (`data/catalog.db` or `catalog.json`): `scans` and `scan_roots` only. Writes: create scan, update scan progress/completion, add/remove roots. Stays small and low write volume.
- **Per-scan store**: One SQLite file per scan, e.g. `data/scans/<id>.db`, containing only the `files` table (and maybe scan metadata columns in a single row). Schema can stay as today; you keep SQL and existing indexes.

**Behavior**:

- **Scan phase**: Write only to `data/scans/<id>.db` (or create it when the scan is created). No contention with other scans or UI.
- **Hash phase**: Only that scan’s DB is written to; 6 workers hit one small DB (still single writer, but no other scans or UI writers). Optionally use a single connection and batch updates if needed.
- **UI – single scan**: Open `data/scans/<id>.db` read-only; run existing duplicate-group and file queries.
- **UI – “All” / across scans**: Resolve “latest scan per root” from catalog; for that set of scan IDs, open each `data/scans/<id>.db` read-only, run duplicate-group queries per DB, merge and deduplicate groups by hash in Go (e.g. merge counts, aggregate file lists). With “not a lot of data” and a small number of scans, opening a few DBs and merging in memory is fine.

**How “duplicates across all folders” works with one DB per scan**: You don’t JOIN across databases. For “latest scan per root” you get N scan IDs from the catalog. Then:

1. For each scan ID, open that scan’s DB read-only and query **all files with `hash_status = 'done'`** (or stream `hash, path, size, …` per file).
2. In Go, build a single **`map[hash][]File`** (or `map[hash][]FileInfo`) and merge: for each hash, append every file from every scan into one slice.
3. Any hash with `len(files) > 1` is a duplicate group — and that group can (and often will) span different roots. So “crossing” different origins is done by **hash** in application memory; no cross-DB join.

So “one DB per scan” *can* support “duplicates across all folders”; the cross is by hash in app, not by a single SQL query. The downside is you must implement and maintain this merge (and pagination over merged groups if you need it).

**Pros**: Keeps SQL and your current query patterns per scan; isolates writers; no cross-scan lock contention; single file per scan is easy to backup/delete.  
**Cons**: Cross-scan “global” view requires explicit merge-by-hash in application code and possibly pagination over merged results; catalog schema and where scan metadata lives must be designed.

---

## Option 2: In-memory state + snapshot persistence

**Idea**: No database during normal operation. Keep scans and files in Go structs (e.g. `map[scanID][]File`, list of scans). Hash phase updates in-memory only; duplicate grouping is in-memory (group by hash / inode). **“Duplicates across all folders” is natural**: one `map[hash][]File` (or similar) over all files from all scans; any hash with more than one file is a duplicate group, regardless of which root each file came from. On shutdown (and optionally on scan completion), persist full state to a file (JSON, MessagePack, or gob); on startup, load from file.

**Pros**: Single global view; no lock contention; very fast; no DB dependency; cross-folder duplicates are one in-memory grouping.  
**Cons**: Memory bound by total file count; persistence is snapshot-based (no incremental WAL); you reimplement the current SQL queries in Go (grouping, pagination, etc.).

Best if total files across all scans comfortably fit in RAM and you’re okay with “load entire state on startup”.

---

## Option 3: Embedded KV (bbolt) – one DB per scan

**Idea**: Same split as Option 1 (catalog + per-scan store), but each scan’s store is a bbolt file instead of SQLite. One bucket per “table” (e.g. `files`), keyed by file id; duplicate grouping = iterate and group in Go. bbolt is single-writer by design but blocks instead of returning BUSY; you control batching.

**Note**: The original Bolt (`github.com/boltdb/bolt`) is deprecated/archived. Use the maintained fork **bbolt** (`go.etcd.io/bbolt`, from etcd-io/bbolt), which is API-compatible and actively maintained.

**Pros**: No SQLITE_BUSY; single file per scan; good for append-heavy then read-mostly.  
**Cons**: No SQL; you reimplement indexing and duplicate queries in Go.

Useful if you want to move away from SQLite entirely but keep the “one store per scan” isolation.

---

## Option 4: PostgreSQL (or another server RDBMS)

**Idea**: Use a real RDBMS in a second container; point the app at it. No single-writer limit; concurrent hash workers and UI reads work without BUSY.

**Pros**: Solves concurrency and query performance; no change to query logic.  
**Cons**: Requires running and maintaining a DB server; conflicts with “single container / no extra services” if you want to keep Ditto as one process (e.g. on Synology). Only makes sense if you’re okay with a second service.

---

## Single global store vs merge in app

- **If you want “duplicates across all folders” to come from one place** (one DB, one query, no custom merge): you need a **single global store** that holds all files from all scans. That rules out “one DB per scan” unless you accept merge-by-hash in Go. The options that give both a single global store and better concurrency are:
  - **Option 2 (in-memory + snapshot)**: One in-memory structure; grouping by hash across all scans is trivial; no DB contention. Fits “not a lot of data” and keeps a single process, no extra services.
  - **Option 4 (PostgreSQL)**: One DB, same query model as today (e.g. `WHERE scan_id IN (...)` and `GROUP BY hash`), no single-writer limit; requires a second container/service.

- **If you’re okay with “cross” implemented as merge-by-hash in application code**, Option 1 (one DB per scan + catalog) keeps SQL per scan and isolates writers; “across all folders” is then: query each scan DB for (hash, file) rows, merge by hash in Go, show groups with count > 1.

---

## Recommendation

- For **duplicates across all folders** as the main goal:
  - Prefer **Option 2 (in-memory + snapshot)** if total file count fits in RAM: one global view, no merge logic, no DB contention, no extra services.
  - If you don’t want to be memory-bound, **Option 4 (PostgreSQL)** gives a single global store and good concurrency in exchange for running a DB server (e.g. second container).
- **Option 1 (one DB per scan)** remains viable only if you accept implementing and maintaining the “merge by hash across scan DBs” path for the global duplicate view.
