# ADR-004: HTMX and Tailwind for the Web UI

**Date**: 2025-02-07

## Decision

**Use HTMX and Tailwind CSS for the Web UI implementation.**

- **HTMX** for dynamic behavior: partial page updates, form submissions that swap content in place, inline expansion of duplicate groups, polling for scan progress, and delete/action flows with optional confirmation. The server returns HTML fragments; no separate JSON API or client-side state layer is required for core flows.
- **Tailwind CSS** for layout and styling: utility classes in templates for dashboard layout, tables, cards, buttons, badges, modals, and loading states. A single Tailwind build (CLI) will scan Go templates, purge unused classes, and produce one CSS file to embed or serve.
- Go backend continues to own the UI via server-rendered HTML (e.g. `html/template` or a small template package); HTMX and Tailwind are added to that stack. Minimal or no custom JavaScript; add vanilla JS or a small library (e.g. Alpine.js) only where HTMX is insufficient.

## Context

ADR-003 chose a Web UI with server-rendered HTML and minimal JavaScript. We needed to pick concrete technologies for interactivity and styling. HTMX was chosen because it fits the interaction model (forms, tables, expand-in-place, polling) without a SPA or heavy JS framework. Tailwind was chosen to ship a consistent, modern-looking UI quickly and to keep styling colocated with markup in templates. Together they support a good UX while keeping the build and image small and the backend as the single source of truth.

## Consequences

- **Positive**
  - No separate front-end framework or build (beyond Tailwind’s CSS build); smaller image and simpler pipeline.
  - Fast iteration: change templates and Tailwind classes; server returns fragments for HTMX.
  - Accessible, progressive enhancement: semantic HTML and ARIA remain our responsibility; HTMX does not block that.
- **Negative**
  - Tailwind requires a build step (e.g. `tailwindcss` CLI) that must run before embedding CSS or shipping assets; content/purge config should include template paths.
  - Highly stateful or real-time client interactions may need a small amount of extra JS; we accept that for the minority of cases.
- **Neutral**
  - If we later need a richer client (e.g. real-time collaboration), we can introduce a targeted JS layer without undoing the HTMX + Tailwind choice for the bulk of the UI.
