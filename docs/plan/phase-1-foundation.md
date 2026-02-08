# Phase 1: Foundation

**Goal:** Go module, project layout, SQLite connection and schema (scans, files), config (data path and port from env). No UI; the app can open the DB and run migrations on startup.

**References:** [ADR-001](../decisions/ADR-001-packaging-strategy.md), [ADR-002](../decisions/ADR-002-sqlite-storage.md), [ADR-005](../decisions/ADR-005-scan-and-hash-pipeline.md).

---

## TDD and review

- Each step is implemented **test-first**: write the test(s), see them fail, then implement until they pass.
- One step = one logical change set. Prefer one PR per step so the reviewer can focus.
- **Review checklist** per step: tests exist and pass; code is minimal; naming and layout match the plan.

---

## Step 1: Go module and directory layout

**What:** Initialize the Go module and create the minimal directory structure. Go is managed with [mise](https://mise.jdx.dev/); the project pins the version in `.go-version`. Run `mise install` in the repo to use that Go version.

**TDD:**
- No meaningful code yet; `go build ./...` and `go test ./...` are enough. After adding the layout and a minimal main, both must succeed.

**Deliverables:**
- `go.mod` with module path (e.g. `github.com/eargollo/ditto`) and Go version.
- Directories: `cmd/ditto/` (main package), `internal/` (for app code). No business logic in `cmd` beyond wiring.
- `cmd/ditto/main.go`: print something and exit (e.g. "ditto" or exit 0). No DB yet.
- Optional: `.gitignore` with `/ditto`, `*.db`, etc.

**Review:** Build and test pass; layout is clear; no unnecessary files.

---

## Step 2: Config from environment

**What:** A config package that reads data directory path and HTTP port from the environment, with sensible defaults.

**TDD:**
- `internal/config/config_test.go`: Test that with no env set, defaults are returned (e.g. data dir `./data` or `/data`, port `8080`).
- Test that when `DITTO_DATA_DIR` and `DITTO_PORT` (or chosen names) are set, they are used.
- Test invalid port (e.g. negative or non-numeric) returns error or is validated.

**Deliverables:**
- `internal/config/config.go`: struct with `DataDir() string`, `Port() int` (or similar); load from env; defaults per ADR-002 (data path configurable, sensible default).
- Use standard env names (e.g. `DITTO_DATA_DIR`, `DITTO_PORT`). Document in code or a small comment.

**Review:** Tests are clear; defaults match ADR; no global state if avoidable.

---

## Step 3: SQLite connection and WAL

**What:** Open SQLite using `modernc.org/sqlite` (pure Go), enable WAL mode, and close cleanly.

**TDD:**
- Test: open with `:memory:`, run `PRAGMA journal_mode` and assert result is `wal` (or enable WAL after open and verify).
- Test: open with a temp file path, create DB file, close; reopen and confirm DB exists.
- Test: close and ensure subsequent use of the connection fails or is safe.

**Deliverables:**
- `internal/db/db.go` (or `internal/store/db.go`): `Open(path string) (*sql.DB, error)`, ensure WAL mode is set after open. Use `database/sql` and driver `modernc.org/sqlite`.
- Caller is responsible for `db.Close()`. No global DB handle.

**Review:** Tests use `:memory:` or temp dir; WAL is enabled; driver is the one from ADR-002.

---

## Step 4: Schema and migrations

**What:** Define and apply the initial schema: `scans` and `files` tables. Schema must support ADR-005 (inode, device, hash status) and ADR-002 (single DB file).

**TDD:**
- Test: run migrations against an in-memory DB; query `sqlite_master` (or equivalent) and assert `scans` and `files` exist.
- Test: insert one row into `scans`, one into `files` (with required columns), then select them back. Ensures columns and types are correct.

**Schema (reference):**
- **scans:** `id` (primary key, e.g. integer auto), `created_at` (datetime), optional `root_path` (text, nullable for now) if we want it for phase 2.
- **files:** `id` (primary key), `scan_id` (FK to scans), `path` (text), `size` (integer), `mtime` (integer or datetime), `inode` (integer), `device_id` (integer, optional), `hash` (text, nullable), `hash_status` (text: `pending` / `hashing` / `done` / `failed`). Indexes: `scan_id`, and later we’ll need `(scan_id, size)` and `hash` for duplicate grouping; can add in this step or the next.

**Deliverables:**
- Migrations: either a single `Migrate(db *sql.DB) error` that runs `CREATE TABLE IF NOT EXISTS ...` for `scans` and `files`, or a small set of SQL files in `internal/db/migrations/` and a runner. Prefer simple: one function that creates both tables.
- Enable foreign keys if desired: `PRAGMA foreign_keys = ON` in Open or Migrate.

**Review:** Schema matches ADR-005 (inode, device_id, hash_status); migrations are idempotent; tests prove tables and one insert/select round-trip.

---

## Step 5: Scan repository

**What:** Create and read scan records (no file tree yet; just DB operations).

**TDD:**
- Test: `CreateScan(ctx, db, rootPath)` returns a scan with non-zero ID and `created_at` set; `GetScan(ctx, db, id)` returns that scan.
- Test: `GetScan` with non-existent ID returns error (e.g. `sql.ErrNoRows`) or a “not found” sentinel.
- Test: `ListScans(ctx, db)` returns scans in deterministic order (e.g. newest first); empty DB returns empty slice.

**Deliverables:**
- `internal/db/scans.go` (or `internal/store/scans.go`): `CreateScan(ctx, db, rootPath string) (*Scan, error)`, `GetScan(ctx, db, id int64) (*Scan, error)`, `ListScans(ctx, db) ([]Scan, error)`.
- `Scan` struct: at least `ID`, `CreatedAt`, `RootPath` (optional for phase 1).

**Review:** Tests are table-driven if useful; context is passed through; no SQL in tests (use the same DB package).

---

## Step 6: File repository

**What:** Insert file records for a scan and query them by scan.

**TDD:**
- Test: insert a file with `InsertFile(ctx, db, scanID, path, size, mtime, inode, deviceID)`, then `GetFilesByScanID(ctx, db, scanID)` returns it with correct fields. Use `hash_status = 'pending'` and `hash = NULL` by default.
- Test: `GetFilesByScanID` for a scan with no files returns empty slice.
- Test (optional): insert multiple files for same scan, query and assert count and contents.

**Deliverables:**
- `internal/db/files.go`: `InsertFile(ctx, db, scanID, path, size, mtime, inode, deviceID)` (or a struct for the args), `GetFilesByScanID(ctx, db, scanID) ([]File, error)`.
- `File` struct: `ID`, `ScanID`, `Path`, `Size`, `MTime`, `Inode`, `DeviceID`, `Hash`, `HashStatus`. Match schema from Step 4.

**Review:** Hash and hash_status defaults are correct; tests cover happy path and empty case.

---

## Step 7: Wire main to config, DB, and migrations

**What:** `main` reads config, opens DB at config data dir (creating the file if needed), runs migrations, then exits (or keeps running for later phases). No HTTP server yet.

**TDD:**
- Option A: integration test that sets `DITTO_DATA_DIR` to a temp dir, runs a function that opens DB and migrates, then checks that the DB file exists and tables exist.
- Option B: no automated test for main; reviewer verifies manually that `go run ./cmd/ditto` with env set creates `./data/ditto.db` (or default path) and that re-run doesn’t error (migrations idempotent).

**Deliverables:**
- `cmd/ditto/main.go`: load config, ensure data dir exists (`os.MkdirAll`), open DB with `file:<dataDir>/ditto.db` (or similar), run migrations, close DB, exit 0. Log or print minimal info (e.g. “Started”, “Migrations OK”) if helpful.
- No HTTP server in this phase.

**Review:** Main is short; config and DB are used correctly; idempotent migrations; data dir creation is correct.

---

## Phase 1 done when

- [ ] All steps implemented with tests first (TDD).
- [ ] `go build ./...` and `go test ./...` pass.
- [ ] Reviewer has approved each step (or each PR).
- [ ] Running the binary with default or env-set `DITTO_DATA_DIR` creates the DB and applies schema; re-run succeeds.
