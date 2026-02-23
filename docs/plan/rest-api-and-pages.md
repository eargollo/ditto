# REST API + frontend uses REST for data (Option 1, simplest)

**Goal:** One Go process serves a REST API (JSON) and HTML **shell** pages. The frontend **retrieves all data** via the REST API (fetch + vanilla JS); no server-rendered data in the initial HTML. No separate frontend app, no framework.

---

## 1. Projected REST API

All API routes live under `/api/`. JSON request/response; standard HTTP methods and status codes.

### 1.1 Scan roots (folders)

| Method | Path | Description | Request | Response |
|--------|------|-------------|---------|----------|
| GET | `/api/roots` | List scan roots | — | `200` `[{ "id": 1, "path": "/data", "created_at": "..." }]` |
| POST | `/api/roots` | Add a root | `{ "path": "/data" }` | `201` `Location: /api/roots/1` + body `{ "id": 1, "path": "/data", "created_at": "..." }` |
| GET | `/api/roots/{id}` | Get one root | — | `200` `{ "id": 1, "path": "/data", "created_at": "..." }` or `404` |
| DELETE | `/api/roots/{id}` | Remove root | — | `204` or `404` |

### 1.2 Scans

| Method | Path | Description | Request | Response |
|--------|------|-------------|---------|----------|
| GET | `/api/scans` | List scans (newest first) | optional `?limit=20` | `200` `[{ "id", "folder_id", "root_path", "created_at", "completed_at", "hash_completed_at", "file_count", "hashed_file_count", "hash_reused_count", ... }]` |
| POST | `/api/scans` | Start a scan | `{ "root_path": "/data" }` or `{ "root_id": 1 }` | `202` `Location: /api/scans/123` + body `{ "id": 123, "root_path": "/data", ... }` (queue full → `503`) |
| GET | `/api/scans/{id}` | Get scan (status, stats) | — | `200` scan object or `404` |
| POST | `/api/scans/{id}/continue` | Re-queue scan (e.g. retry hash) | — | `202` or `302` to `/api/scans/{id}` if already complete, or `404` / `503` |

### 1.3 Scan status (for polling)

| Method | Path | Description | Response |
|--------|------|-------------|----------|
| GET | `/api/scans/{id}/status` | Lightweight status for polling | `200` `{ "id", "completed_at", "hash_completed_at", "file_count", "hashed_file_count", "hash_reused_count", "hash_error_count" }` or `404` |

### 1.4 Duplicates (precomputed global view — home page)

| Method | Path | Description | Request | Response |
|--------|------|-------------|---------|----------|
| GET | `/api/duplicates/summary` | Global summary (group count, total files, reclaimable) | — | `200` `{ "group_count", "total_files", "total_size", "reclaimable_size" }` |
| GET | `/api/duplicates/groups` | Paginated duplicate-by-hash groups **with files embedded** | `?limit=20&offset=0&max_files_per_group=10` | `200` `{ "groups": [...], "total": N }` each group: `{ "hash", "file_count", "total_size", "reclaimable_size", "files": [{ "id", "path", "size", ... }] }`. Embedding files avoids n+1; `max_files_per_group` caps list size (default 10); truncated groups can use drill-down. |
| POST | `/api/duplicates/groups/refresh` | Refresh **one** duplicate group: check each file on disk; mark missing as deleted, flag changed files for rehash; update group row (or remove if no duplicates left). | `{ "hash": "<content-hash>" }` | `200` `{ "status": "ok" }` or `4xx`/`5xx` `{ "error": "..." }` |
| GET | `/api/duplicates/groups/{hash}/files` | Full file list for one group (drill-down when embedded list was truncated) | — | `200` `[{ "id", "path", "size", "folder_id", ... }]` or `404` *(optional; not yet implemented)* |

### 1.5 Scan export

| Method | Path | Description | Response |
|--------|------|-------------|----------|
| GET | `/api/scans/{id}/export` | Export scan files as CSV (path, hash, size) | `200` CSV body, `Content-Disposition: attachment` (or `404` if scan not found) |

### 1.6 Admin

| Method | Path | Description | Request | Response |
|--------|------|-------------|---------|----------|
| POST | `/api/admin/refresh-duplicate-groups` | Rebuild the entire duplicate groups table (TRUNCATE + INSERT from all hashed files). Use after data restore or if the list looks wrong. Normal flow uses incremental updates. | — | `200` `{ "status": "ok" }` or `5xx` `{ "error": "..." }` |

### 1.7 Health

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/health` or keep `/health` | `200` `ok` / `503` + body |

### 1.8 Conventions

- **Errors:** `4xx`/`5xx` with body e.g. `{ "error": "message" }` (or `{ "code": "...", "message": "..." }`).
- **Timestamps:** RFC3339 in JSON.
- **Optional:** `Accept: application/json` on existing HTML routes could return JSON when requested (content negotiation); not required for v1.

---

## 2. Frontend: shell pages + REST for data

- **Pages are shells:** Go serves HTML that is **layout only** (nav, title, one or more target containers like `<main id="content">` or `<div id="scans-list">`). No scan list, no duplicate groups, no scan status embedded in the initial HTML.
- **Data comes from the API:** A small **vanilla JS** script (e.g. `static/app.js`) runs on each page. It:
  - Chooses which API to call based on the route (e.g. `pathname === '/scans'` → `GET /api/scans` + `GET /api/roots`; `pathname.match(/^\/scans\/(\d+)$/)` → `GET /api/scans/{id}` and optionally `GET /api/scans/{id}/status` for polling).
  - Uses `fetch("/api/...")`, then `response.json()`.
  - Renders the JSON into the target container(s) by building HTML strings (e.g. template literals: `data.scans.map(s => \`<tr>...</tr>\`).join('')`) and setting `container.innerHTML = ...`.
- **Polling:** For scan progress, the script can `setInterval(() => fetch("/api/scans/123/status").then(...).then(data => { ... update status section ... }), 2000)` and stop when `hash_completed_at` is set.
- **Mutations (start scan, add root, continue):** Can stay as **form POST** to **page** endpoints that return redirects (server shares logic with `POST /api/scans` etc.), so no JS required for submit. Optionally later: forms could POST to the API with JS and then set `window.location = response.headers.get('Location')`.

### 2.1 Tech choices

| Item | Choice |
|------|--------|
| **Templating (shell)** | Go `html/template`: layout, nav, empty container(s), script tag for `app.js`. |
| **Data fetching** | Vanilla JS `fetch()` to `/api/*`. |
| **Rendering** | JS builds HTML from JSON (template literals or small helpers). No build step. |
| **Framework** | None. No React, Vue, Svelte, HTMX for data. |
| **Styling** | Unchanged (e.g. Tailwind via static CSS). |

### 2.2 What we are *not* introducing

- No SPA framework.
- No separate frontend repo or bundler (Vite, webpack).
- No client-side router; navigation is full-page (links) or form POST + redirect.

---

## 3. Summary

| Layer | Tech |
|-------|------|
| **API** | REST under `/api/`: JSON in/out; all data and mutations available here. |
| **Pages** | Same Go process; HTML shells from `html/template`; one vanilla JS file that fetches from `/api/*` and renders into the shell. |
| **Mutations (optional v1)** | Form POST to page endpoints (redirect); or JS POST to API + redirect. |
| **Styling** | Unchanged (e.g. Tailwind). |

**Next steps:** Implement `/api/*` routes; replace current server-rendered data with shell pages + `static/app.js` that calls the API and renders; add integration tests against the API.
