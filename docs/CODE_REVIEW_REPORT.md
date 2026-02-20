# Code Review Report: Interfaces, Abstractions, Coupling & Decoupling

**Scope:** `cmd/ditto`, `internal/config`, `internal/db`, `internal/hash`, `internal/scan`, `internal/server`, `internal/integration`  
**Focus:** Interfaces, abstractions, types, coupling, and decoupling. No code changes—findings and recommendations only.

---

## Classification

- **Critical:** Affects correctness, testability, or major refactors; should be addressed soon.
- **High:** Important for maintainability, clarity, or avoiding future debt.
- **Medium:** Nice to have; improves structure or reduces duplication.
- **Low:** Minor consistency or style improvements.

---

## 1. Interfaces & Abstractions

### 1.1 No repository/data access interface (Critical)

**Finding:** All persistence is done via package-level functions that take `*sql.DB` and `context.Context`. There is no interface abstracting the database layer.

**Impact:**

- **Testing:** Handlers and business logic cannot be unit-tested without a real DB or a large test helper (e.g. `TestPostgresDB`). No way to inject a fake store.
- **Flexibility:** Swapping or doubling storage (e.g. read replica, different DB) would require touching every call site.
- **Boundaries:** The “contract” of what the app needs from storage is not explicit; it’s implied by many `db.*` calls.

**Example:** `server.Server` and `hash.RunHashPhase` depend directly on `*sql.DB` and `db` package functions. There is no `ScanStore`, `FileStore`, or `DuplicateStore` interface.

**Recommendation:** Introduce small interfaces (e.g. `ScanStore`, `FileStore`) that describe the operations each layer needs. Implement them with structs that hold `*sql.DB` and delegate to existing `db` functions. Inject these into `Server` and hash/scan runners so tests can inject mocks or fakes.

---

### 1.2 Config is a concrete struct, not an interface (High)

**Finding:** `config.Config` is a struct with getters (`Port()`, `DatabaseURL()`). No interface abstracts configuration.

**Impact:** Tests that need different configs (e.g. port 0) must use the real `config.Load()` and often env vars. No clean way to inject “test config” without touching the environment.

**Recommendation:** If you introduce dependency injection for the server, consider a small `Config` interface (e.g. `Port() int`, `DatabaseURL() string`) and have the concrete `*config.Config` implement it so tests can inject a stub config.

---

### 1.3 Scan/hash entry points take `*sql.DB` (High)

**Finding:** `scan.RunScan`, `scan.RunScanForExisting`, `scan.RunPipeline`, and `hash.RunHashPhase` all take `*sql.DB` as a parameter. They are not behind an abstraction that could be swapped for testing or for a different backend.

**Impact:** Integration-style tests (real DB) are the only way to test these flows. Any “run a scan with a fake DB” scenario is not supported.

**Recommendation:** Align with the repository interface idea: have scan/hash accept a minimal interface (e.g. “create scan, upsert files, update scan completed”) rather than `*sql.DB`, so you can test the pipeline logic with an in-memory or mock implementation.

---

## 2. Types & Naming

### 2.1 Folder vs ScanRoot duplication (Medium)

**Finding:** `db.Folder` and `db.ScanRoot` are identical structs (ID, Path, CreatedAt). `scanroots.go` exists only to map folders to “ScanRoot” for “API compatibility” and duplicates `ListFolders`→`ListScanRoots`, `GetFolder`→`GetScanRoot`, etc.

**Impact:** Two names for the same concept; every new folder operation must be mirrored in scanroots. Risk of drift (e.g. one path returns something different).

**Recommendation:** Pick one canonical type (e.g. `Folder`) and use it everywhere. If the API must expose a name like “ScanRoot”, keep it only in the API layer (e.g. in `server` or a dedicated API package) as a view/DTO of `Folder`, not as a second DB type.

---

### 2.2 DuplicateGroupByHash used in two different contexts (Medium)

**Finding:** `db.DuplicateGroupByHash` is used both for “scan-scoped” duplicate groups (from `files` + `file_scan`) and for “precomputed” groups (from `duplicate_groups_hash`). The precomputed view has an extra `reclaimable_size` column that is read in `precompute.go` but the struct only has `Hash`, `Count`, `Size`; reclaimable is dropped in `DuplicateGroupsHashPaginated`.

**Impact:** The same type represents two slightly different queries (with and without reclaimable). Callers that need reclaimable (e.g. API) recompute it (e.g. in `apiDuplicatesGroups`: `reclaimable = g.Size - g.Size/g.Count`), which duplicates logic and can diverge from the precomputed value.

**Recommendation:** Either extend `DuplicateGroupByHash` with an optional `ReclaimableSize` and populate it from the precomputed table where available, or introduce a separate type for “precomputed group” that includes reclaimable and use it in the API and home page.

---

### 2.3 listScans omits DeletedAtUpdateDurationMs (Medium)

**Finding:** `GetScan` in `scans.go` selects `deleted_at_update_duration_ms` and maps it to `Scan.DeletedAtUpdateDurationMs`. The private `listScans` does not include this column; its query only has `file_count` through `hash_error_count`. So `ListScans` / `ListScansRecent` return scans without `DeletedAtUpdateDurationMs` populated.

**Impact:** Any UI or API that uses list results and expects `DeletedAtUpdateDurationMs` will see it as nil even when it is set in the DB. Inconsistent with `GetScan`.

**Recommendation:** Add `deleted_at_update_duration_ms` to the `listScans` SELECT and scan it into `s.DeletedAtUpdateDurationMs` (with a `sql.NullInt64` and the same mapping as in `GetScan`) so list and get are consistent.

---

## 3. Coupling

### 3.1 Server tightly coupled to db, config, scan, and hash (Critical)

**Finding:** `internal/server` imports `internal/db`, `internal/config`, `internal/scan`, and `internal/hash`. The server:

- Constructs and runs scans and hash phase itself (`runOneScan` → `scan.RunScanForExisting`, `hash.RunHashPhase`).
- Calls many `db.*` functions directly (scans, files, roots, duplicates, precompute).
- Knows about DB types (`db.Scan`, `db.File`, `db.DuplicateGroupByHash`, etc.) and template/API shapes.

**Impact:** The HTTP layer is also the “orchestrator” and the main consumer of the DB. Changing storage or moving “run scan” behind a job queue would require changing the server and possibly duplicating logic. Hard to test “just the HTTP API” without a real DB and real scan/hash.

**Recommendation:** Introduce an application/service layer (e.g. `internal/app` or `internal/service`) that owns “start scan”, “continue scan”, “run hash phase”, and “get duplicates”. The server would only do HTTP (parse request, call service, format response). Service would depend on abstractions (e.g. ScanStore, ScanRunner) rather than raw `*sql.DB`. That reduces coupling and makes both the server and the service testable in isolation.

---

### 3.2 main.go knows too much about wiring (High)

**Finding:** `main.go` handles: reference mode, config load, data dir creation, DB open, migrate, backfill, scan CLI, server creation, signal handling, and starting the server. It also knows that “scan” needs `scan.OptionsForRoot`, `scan.RunScan`, and `hash.RunHashPhase` with `Workers: 6`.

**Impact:** Adding a new mode or changing how scan/hash are invoked (e.g. from a worker process) would require editing `main.go`. The “application bootstrap” is mixed with “orchestration details.”

**Recommendation:** Move “run scan for root” and “run hash for scan” into a dedicated runner or service (used by both CLI and server). Main should only: parse top-level mode, load config, open DB, run migrations/backfill, and delegate to a “run server” or “run scan once” function that takes config and DB (or interfaces). That keeps main thin and keeps orchestration in one place.

---

### 3.3 Hash package coupled to db types and SQL (High)

**Finding:** `hash.RunHashPhase` and helpers use `db.File`, `db.HashUpdate`, `db.ForEachPendingHashJobGlobal`, `db.UpdateFileHashBatch`, `db.HashForInode`, `db.HashForInodeFromPreviousScan`, `db.HashForPathSize`, `db.ResetFileHashStatusToPending`, `db.PrecomputeDuplicateGroupsHash`, and various scan/filed DB updates. The hash package is the main “business” logic for the hash phase but is tightly bound to the DB layer.

**Impact:** Testing “hashing strategy” (reuse inode, reuse path+size, then read file) requires a real DB or a lot of mocking at the `db` boundary. The hash package cannot be reused in a different context (e.g. a standalone hasher that writes elsewhere) without bringing in the whole DB layer.

**Recommendation:** Introduce a small interface for “hash job source” and “hash result sink” (e.g. iterate jobs, report hash/reused/error). The current implementation would use `db.*` behind that interface. Then the core “process one file: try reuse, else hash” logic can be tested with a fake source/sink, and the DB becomes one implementation detail.

---

### 3.4 Scan pipeline coupled to db and filesystem (High)

**Finding:** `scan.RunPipeline` takes `*sql.DB`, `scanID`, `folderID`, paths, and options. It uses `db.UpsertFilesBatch`, `db.InsertFileScanBatch`, `db.UpdateScanFileCountProgress`, and `db.UpdateScanCompletedAt` (by the caller). The pipeline is “walk dirs + write to this DB”; there is no abstraction over “where to write” or “how to list files.”

**Impact:** Testing “pipeline behavior” (backpressure, progress updates, error handling) without a real DB and real filesystem is difficult. The pipeline is the main place where FS and DB meet; that coupling is explicit and hard to substitute.

**Recommendation:** Consider a “file sink” interface (e.g. `UpsertFiles(ctx, folderID, scanID, entries) ([]int64, error)`, `LinkToScan(ctx, fileIDs, scanID) error`) and optionally a “directory lister” interface. The pipeline would depend on these interfaces; the default implementation would call `db.*`. Tests could use an in-memory sink and a fake tree to assert on ordering and batching without Postgres.

---

## 4. Decoupling & Duplication

### 4.1 Two ID parsers in server (Medium)

**Finding:** `server/server.go` defines `parseScanID(idStr string) (int64, error)` and `server/api.go` defines `parseID(idStr string) (int64, error)`. Both do `strconv.ParseInt(idStr, 10, 64)`. API handlers use `parseID` for root IDs and `parseScanID` for scan IDs; the behavior is identical.

**Impact:** Duplicate code and two names for the same parsing. If you add validation (e.g. positive-only), you have to remember to update both.

**Recommendation:** Use a single helper (e.g. `parseInt64` or `parseID`) in one place (e.g. a small `server/httputil` or in the same file as other shared helpers) and use it for both root and scan IDs. Optionally add a wrapper or different name for “scan ID” only if you want to add scan-specific validation later.

---

### 4.2 HTML and JSON health handlers duplicate logic (Low)

**Finding:** `handleHealth` (GET /health) and `apiHealth` (GET /api/health) both check `s.db != nil`, then `s.db.PingContext(r.Context())`, and return 503 on error or 200 with "ok". The only difference is that the API handler also checks `r.Method == GET` and uses `writeAPIError` for method not allowed.

**Impact:** Minor duplication; if health check logic changes (e.g. add a check for precompute freshness), both paths must be updated.

**Recommendation:** Extract a single “health check” function that returns an error (e.g. `err != nil`) and have both handlers call it and then format the response (HTML vs JSON) as needed.

---

### 4.3 Form vs JSON “create scan” and “create root” (Medium)

**Finding:** Creating a scan is implemented twice: once in `handleScansStart` (form: `root_path`, `root_id`) and once in `apiScansCreate` (JSON: `root_path`, `root_id`). Same for roots: `handleScanRootsAdd` (form `path`) and `apiRootsCreate` (JSON `path`). The business logic (get/create folder, create scan, enqueue) is duplicated with small differences in how the request body is read and how errors are returned (redirect vs JSON).

**Impact:** Bug fixes and behavior changes (e.g. validation, queue full handling) must be applied in two places. Risk of the two code paths diverging.

**Recommendation:** Extract a single “create scan” function that takes (ctx, db, rootPath or rootID, enqueue function) and returns (scanID, error). Both handlers parse their request (form vs JSON), then call this function and handle the result (redirect or JSON). Same idea for “add root”: one function `AddRoot(ctx, db, path) (id, error)` used by both handlers.

---

## 5. Package Layout & Dependencies

### 5.1 SQLite code in production path (High)

**Finding:** `internal/db/db.go` implements `Open(path)` and `OpenReadOnly(path)` for SQLite (with WAL, busy timeout). `cmd/ditto/main.go` only uses `db.OpenPostgres` and `db.MigratePostgres`; no SQLite in the main binary. SQLite is still used in `db_test.go` and `migrate_test.go` (e.g. `Open(":memory:")`, `sqlite_master`). Other DB code (e.g. `scans.go`, `files.go`) uses Postgres-style placeholders (`$1`, `$2`) and is not SQLite-compatible.

**Impact:** New contributors may think the app supports SQLite. The SQLite path is effectively legacy or test-only; keeping it in the same package as Postgres adds noise and potential confusion. Migrate tests are SQLite-specific and don’t exercise the real Postgres migration.

**Recommendation:** Either: (a) move SQLite helpers to a subpackage (e.g. `internal/db/sqlite`) or a build-tagged file used only by tests, and document that production is Postgres-only; or (b) remove SQLite from the main tree and use Postgres (e.g. testcontainers or a shared test DB) for migration tests so they run against the real schema.

---

### 5.2 reference mode and scan package (Medium)

**Finding:** “Reference” mode (`ditto reference <root>`) lives in `main.go` and uses `scan.OptionsForRoot`, `scan.ReferenceCSV`, and `scan` types (`ReferenceStats`, exclude patterns). It does not use the DB. The scan package thus serves two masters: (1) DB-backed scan pipeline and (2) reference CSV generation (no DB). Both use `scan.Entry`, `scan.Walk`, and exclude logic, but reference uses `hash.HashFile` directly and its own in-memory candidate logic.

**Impact:** The scan package has two entry points (RunScan/RunPipeline vs ReferenceCSV) and some shared types. Coupling is mostly one-way (reference → scan, reference → hash), which is acceptable, but the “scan” package name suggests “DB scan” only; the reference flow is a different use case.

**Recommendation:** Document clearly that the scan package supports both “scan to DB” and “reference CSV” and keep shared types (e.g. `Entry`, `Walk`, exclude) in scan. If the reference flow grows (e.g. more output formats), consider a small `reference` subpackage under `scan` or a top-level `cmd/reference` that uses scan + hash, so that “scan” doesn’t accumulate too many responsibilities.

---

## 6. Error Handling & Consistency

### 6.1 isNotFound in api.go (Low)

**Finding:** `isNotFound(err)` in `api.go` checks `errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows")`. The second condition is fragile (depends on driver error text). Other packages may return different errors for “not found.”

**Impact:** If a different DB layer or driver is used, “not found” might not be recognized and the API could return 500 instead of 404.

**Recommendation:** Prefer a single “not found” contract: either define a sentinel error in `db` (e.g. `var ErrNotFound = errors.New("not found")`) and have DB functions wrap it, or document that callers must use `errors.Is(err, sql.ErrNoRows)` and ensure all DB code uses that. Avoid relying on substring matching on error messages.

---

### 6.2 Ignored errors in handlers (Medium)

**Finding:** Several handlers ignore errors or use `_`: e.g. `handleDuplicates` ignores errors from `DuplicateGroupsByHash` and `DuplicateGroupsByInode` (`byHash, _ := ...`); `handleDuplicateHashGroup` ignores errors from `ListScansRecent` and `FilesInHashGroupAcrossScans` in the scanID==0 branch. That can hide real failures and show empty or wrong data.

**Impact:** Users may see “no duplicates” or wrong data when the cause is a DB or context error. Debugging is harder because failures are silent.

**Recommendation:** At least log errors when ignoring them; ideally return 500 or 503 and a safe message to the user so that monitoring and logs show the failure. Reserve “ignore error and return empty” only for cases where “empty” is a defined fallback and the error is explicitly documented (e.g. “if list fails, show empty list”).

---

## 7. Summary Table

| ID   | Topic                          | Severity  |
|------|---------------------------------|-----------|
| 1.1  | No repository/data interface   | Critical  |
| 1.2  | Config not abstracted          | High     |
| 1.3  | Scan/hash take *sql.DB         | High     |
| 2.1  | Folder vs ScanRoot duplication | Medium   |
| 2.2  | DuplicateGroupByHash dual use  | Medium   |
| 2.3  | listScans missing column       | Medium   |
| 3.1  | Server coupled to db/scan/hash | Critical |
| 3.2  | main.go orchestration          | High     |
| 3.3  | Hash coupled to db             | High     |
| 3.4  | Scan pipeline coupled to db   | High     |
| 4.1  | Two ID parsers                  | Medium   |
| 4.2  | Health handler duplication     | Low      |
| 4.3  | Form vs JSON create scan/root   | Medium   |
| 5.1  | SQLite in production path      | High     |
| 5.2  | reference vs scan package      | Medium   |
| 6.1  | isNotFound fragility           | Low      |
| 6.2  | Ignored errors in handlers      | Medium   |

---

**Suggested order of work (by impact):**

1. **Critical:** Introduce minimal store interfaces and an app/service layer so server and scan/hash depend on abstractions, not `*sql.DB` and global `db.*` (addresses 1.1, 3.1, and partly 3.2, 3.3, 3.4).
2. **High:** Unify or isolate SQLite (5.1), then reduce coupling of hash (3.3) and scan pipeline (3.4) to the DB via the new interfaces.
3. **Medium:** Fix listScans column (2.3), consolidate Folder/ScanRoot (2.1), unify create-scan/create-root handlers (4.3), clarify DuplicateGroupByHash usage (2.2), and improve error handling where errors are ignored (6.2).
4. **Low:** Single ID parser (4.1), shared health check (4.2), and more robust isNotFound (6.1).
