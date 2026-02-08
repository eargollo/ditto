# ADR-003: Web UI as primary user experience

**Date**: 2025-02-07

## Decision

**Use a Web UI as the primary way users interact with Ditto.**

- The application will expose an HTTP server (port configurable, with a sensible default).
- Users access Ditto by opening a URL in a browser (e.g. `http://nas:8080` or via an “Open” link when running as Docker-in-SPK).
- The Web UI will support: configuring scan roots, starting and monitoring scans, browsing duplicate groups, and acting on duplicates (e.g. choosing which file to keep, deleting or moving the rest).
- Implementation approach: Go backend serving the UI; server-rendered HTML with minimal JavaScript (or a lightweight approach such as HTMX) for the first version to keep the image small and avoid a separate front-end build. A richer front-end stack may be considered later if needed.

## Context

Ditto needs an interface for configuring scans, viewing results, and taking action on duplicate files. Options considered included: Web UI, CLI only, hybrid Web + CLI, TUI, desktop app, and DSM-native UI. A Web UI was chosen because it matches how Synology users already work (DSM and most packages are web-based), requires no client install, works from any device on the network, and fits the Docker/SPK model (single port, optional “Open” in Package Center). CLI or other interfaces may be added later as a complement.

## Consequences

- **Positive**
  - Single entry point for all users; no per-device install.
  - Aligns with Synology ecosystem and with Docker port mapping and future SPK “Open” link.
  - Server-rendered UI keeps the build and image simple and avoids a separate Node/front-end pipeline for v1.
- **Negative**
  - We must design and implement authentication and, where applicable, TLS (e.g. reverse proxy) so the UI is not open to the local network without protection.
  - The app must expose and document the HTTP port for Docker and SPK.
- **Neutral**
  - A CLI or API that shares the same backend and SQLite data can be added later without changing this decision.
