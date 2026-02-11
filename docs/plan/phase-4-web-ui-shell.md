# Phase 4: Web UI shell

**Goal:** HTTP server, Tailwind build, HTMX, base layout and static assets. Placeholder pages. No auth (v1). Configurable port.

**References:** [ADR-003](../decisions/ADR-003-web-ui.md) (Web UI primary, HTTP server, configurable port), [ADR-004](../decisions/ADR-004-htmx-tailwind.md) (HTMX, Tailwind, server-rendered HTML).

---

## TDD and review

- Each step is implemented **test-first** where applicable (handler tests, build verification); UI steps may use manual verification.
- One step = one logical change set. Prefer one PR per step.
- **Review checklist:** Tests or build pass; code is minimal; layout and routes are clear.

---

## Step 1: HTTP server and minimal home page

**What:** Start an HTTP server on the configured port (from config). Serve a minimal home page (e.g. "Ditto" and a link to scans). Server runs until shutdown (SIGINT/SIGTERM); main no longer exits immediately after migrations. No Tailwind or HTMX yet.

**TDD:**
- Test: start server on a random port (or test port), GET / returns 200 and body contains "Ditto" (or similar). Shutdown server in test cleanup.
- Optional: test that server binds to config.Port() when no env override.

**Deliverables:**
- `internal/server` (or `internal/http`): `Server` struct with `Run(ctx) error` that listens on config port and serves routes. `NewServer(cfg, db)` or similar.
- Handler for `GET /`: write a minimal HTML page (inline string or very simple template) with "Ditto" and a link to `/scans`.
- `main`: after migrations, create server, run it (blocking); on signal or context cancel, shutdown gracefully (Shutdown(ctx) with timeout).
- Document that no auth in v1 (ADR-003).

**Review:** Server starts and responds; main blocks until shutdown; port from config.

---

## Step 2: Tailwind CSS build

**What:** Add Tailwind CSS: config file, source CSS, and a build step that produces one output CSS file. Embed the built CSS in the app or serve it as a static file. Tailwind content/purge should include template paths so only used classes are in the output.

**TDD:**
- Build: run `tailwindcss` (or npm script) and assert `dist` or `static` contains a CSS file. Can be a Makefile target or npm script; CI can run it before `go build`.
- Optional: test that the served page includes a link to the CSS and that the CSS is non-empty.

**Deliverables:**
- `tailwind.config.js` (or `tailwind.config.ts`): content includes `./internal/server/templates/**/*.html` (or wherever templates live), theme extend if needed.
- Source CSS file (e.g. `web/input.css` or `internal/server/static/input.css`) with `@tailwind base; @tailwind components; @tailwind utilities;`.
- Build: `npx tailwindcss -i ./web/input.css -o ./web/static/app.css` (or similar). Document in README or Makefile.
- Server serves `/static/app.css` from the built file (or embed with `//go:embed`).

**Review:** Tailwind build runs and produces CSS; server serves it or embeds it; purge reduces size.

---

## Step 3: Base layout template

**What:** A single base HTML layout (DOCTYPE, html, head, body). Head includes Tailwind CSS link and a title block. Body has a simple nav/shell (e.g. "Ditto" brand, link to Home, link to Scans) and a main content block. Use Go `html/template` and execute the layout with a content block for each page.

**TDD:**
- Test: handler renders layout and the response contains expected nav text and a main block (e.g. a placeholder "Welcome").
- Test: 404 for unknown path returns 404 status and still uses layout (or a simple error page).

**Deliverables:**
- `internal/server/templates/` (or `web/templates/`): `layout.html` with `{{block "content" .}}...{{end}}` and `{{template "title" .}}`.
- Template func map if needed (e.g. for escaping, or no extra funcs for now).
- Home handler renders layout with a "Welcome" or "Home" content block.
- Router or mux: register GET / and GET /scans (placeholder), and a catch-all or 404 handler.

**Review:** Layout is reused; nav is consistent; Tailwind classes can be used in layout (e.g. container, flex).

---

## Step 4: HTMX script and one interactive placeholder

**What:** Add the HTMX script to the layout (CDN or vendored). One placeholder page (e.g. `/scans` or a dedicated `/demo`) that uses HTMX: e.g. a button that does `hx-get` and swaps a div with the response. Proves HTMX is loaded and server can return fragments.

**TDD:**
- Test: GET /scans returns 200 and HTML containing `hx-get` or `hx-` attribute.
- Test (optional): GET the HTMX target URL (e.g. /scans/list fragment) returns a small HTML fragment; or test with a real browser / Playwright in a later phase.

**Deliverables:**
- Layout includes `<script src="https://unpkg.com/htmx.org@1.9.10"></script>` (or vendored copy) in head or body.
- Placeholder page with e.g. a "Load" button and a div; button has `hx-get="/api/fragment" hx-swap="innerHTML" hx-target="#target"`; handler for `/api/fragment` returns a simple HTML fragment (e.g. "<p>Loaded</p>").
- Document that HTMX is used for partial updates in Phase 5.

**Review:** HTMX script loads; one partial update works; ready for Phase 5 polling and forms.

---

## Step 5: Static assets and health

**What:** Serve static files from a dedicated path (e.g. `/static/`) for CSS and any future JS. Add a health or readiness route (e.g. `GET /health` or `/ready`) that returns 200 and optionally checks DB ping. Useful for Docker/Kubernetes.

**TDD:**
- Test: GET /static/app.css returns 200 and Content-Type text/css (if not embedded).
- Test: GET /health returns 200 (and optionally body "ok" or JSON `{"status":"ok"}`).

**Deliverables:**
- Static file server for `/static/*` pointing to the built Tailwind output (or embed and serve from memory).
- `GET /health`: 200, optional DB ping; no auth.
- 404 handler for unknown routes (returns 404 with minimal HTML or plain text).

**Review:** Static assets and health are reachable; 404 does not leak stack traces.

---

## Phase 4 done when

- [ ] HTTP server runs on config port; main blocks until shutdown; graceful shutdown on signal.
- [ ] Tailwind build produces one CSS file; layout uses it.
- [ ] Base layout template with nav; home and placeholder scans page.
- [ ] HTMX included; one placeholder interaction (e.g. load fragment) works.
- [ ] /static and /health and 404 behave as specified.
- [ ] `go build ./...` (and optional `npm run build:css` or Makefile) succeeds.
