# Release 1, Phase 6: Synology releasable

**Goal:** Docker image, packaging, release process, and documentation so the app can be deployed and used on Synology. Establishes a repeatable way to ship and test; enables iteration (e.g. UX refinements in phase 7) via minor releases.

**Context:** After phases 1–5 the app runs locally: scan roots, scan + hash, view duplicates in the UI. Phase 6 makes it deployable: a container image, a clear release process, and instructions for running on Synology (Container Manager). No SPK yet (per ADR-001).

**References:** [ADR-001](../decisions/ADR-001-packaging-strategy.md) (Docker first; Docker-in-SPK later). Config uses `DITTO_DATA_DIR` and `DITTO_PORT` from env.

---

## TDD and review

- Docker: build image, run container with env and volume, confirm UI is reachable and data persists across restarts.
- Release: produce an image (and optionally binaries) for linux/amd64 and linux/arm64; document how to run it.
- Docs: a new user can follow the guide to run Ditto on Synology and open the UI.

---

## Step 1: Dockerfile and image build

**What:** Add a Dockerfile that builds the Go binary and runs it. Image should be small (multi-stage build), run as non-root where possible, and respect env for data dir and port.

**Details:**
- **Multi-stage:** Stage 1: build from `go.mod` (e.g. `FROM golang:1.22-alpine AS builder`), copy source, `go build -o /ditto ./cmd/ditto`. Stage 2: minimal runtime (e.g. `FROM alpine:3.19` or `scratch` with ca-certs if HTTPS is needed later), copy binary and optional assets (templates, static, default.dittoignore). If using `scratch`, ensure the app does not require shell or extra libs.
- **Entrypoint:** `CMD ["/ditto"]` or `ENTRYPOINT ["/ditto"]`. The app reads `DITTO_DATA_DIR` (default `./data`) and `DITTO_PORT` (default 8080). In Docker, recommend `DITTO_DATA_DIR=/data` and expose a volume at `/data`.
- **User:** Run as non-root (e.g. `USER 1000:1000` or named user) so file permissions are predictable; document that on Synology, mounting host volumes may require matching UID/GID for scan access.
- **Expose:** `EXPOSE 8080` (or the port the app uses).

**Deliverables:**
- `Dockerfile` at repo root (or in `build/` if preferred).
- `.dockerignore` so build context excludes `.git`, `data/`, large or irrelevant files.
- README or `docs/docker.md`: how to build (`docker build -t ditto .`) and run with `-e DITTO_DATA_DIR=/data -e DITTO_PORT=8080 -v ditto-data:/data -p 8080:8080`. Optional: how to mount a folder to scan (e.g. `-v /volume1/photos:/scan:ro` and document that scan roots would be under `/scan` inside the container).

**Review:** `docker build` succeeds; `docker run` with a data volume and port mapping brings up the UI; after restart with the same volume, data (DB) persists.

---

## Step 2: Release process and versioning

**What:** Define how to cut a release: version identifier, build artifacts (Docker image; optional standalone binaries), and where they are published.

**Details:**
- **Version:** Use semantic versioning (e.g. v0.1.0). Version can be set at build time via `-ldflags "-X main.Version=v0.1.0"` and exposed in the UI (footer or /health) for support; optional for phase 6.
- **Build:** Document the steps to produce a release:
  - Build Docker image for `linux/amd64` and `linux/arm64` (Synology: x64 and ARM NAS). Use `docker buildx build --platform linux/amd64,linux/arm64 -t ditto:VERSION .` (or a single platform for local testing).
  - Optional: build standalone binaries with `GOOS=linux GOARCH=amd64 go build -o ditto-linux-amd64 ./cmd/ditto` (and arm64) for users who prefer no Docker.
- **Publishing:** Push image to a registry (Docker Hub, GitHub Container Registry, or private). Document: `docker tag ditto:v0.1.0 user/ditto:v0.1.0` and `docker push user/ditto:v0.1.0`. No automation required for phase 6; a simple checklist in `docs/release.md` or the README is enough.
- **Git:** Tag releases in git (e.g. `git tag v0.1.0`) so the release is reproducible.

**Deliverables:**
- `docs/release.md` (or a "Release process" section in README): steps to build the image for both platforms, tag, and push; optional binary build; git tag.
- Optional: `Makefile` or `scripts/build-release.sh` that runs the build and tag steps to reduce human error.

**Review:** Following the doc produces a multi-platform (or single-platform) image and optionally binaries; image runs on amd64 and arm64 (or document which Synology models are supported).

---

## Step 3: Synology deployment guide

**What:** Documentation so a user can run Ditto on a Synology NAS using Container Manager (Docker).

**Details:**
- **Prerequisites:** Container Manager (or Docker) installed; one or more shared folders to scan.
- **Image:** How to pull the image (e.g. from Docker Hub or GHCR). If the image is not public, how to log in.
- **Create container:** 
  - Image name and tag.
  - Environment: `DITTO_DATA_DIR=/data`, `DITTO_PORT=8080` (or match host port).
  - Volume 1: persistent data (e.g. host path `Docker/ditto/data` or named volume → container `/data`).
  - Volume 2 (and more): folders to scan, e.g. `volume1/Photos` → `/scan/Photos` (read-only recommended). Document that in the UI the user adds scan roots like `/scan/Photos` (paths inside the container).
  - Port: map container 8080 to a host port (e.g. 8080 or 32480).
  - Optional: resource limits (CPU/memory) if needed for large scans.
- **Permissions:** Note that if the container runs as non-root, host-mounted volumes may need readable permissions for that UID/GID; link to or briefly explain Synology shared folder permissions if relevant.
- **Access:** After starting, open `http://<nas-ip>:<mapped-port>` to use the UI.

**Deliverables:**
- `docs/synology.md` (or "Running on Synology" in README): step-by-step for Container Manager: create project/container, set env, mount volumes (data + scan paths), set port, start, open UI, add scan root(s) using container paths.
- Optional: `docker-compose.yml` example (in repo or docs) that users can adapt: service with image, env, volumes, port; comment that Synology Container Manager can import compose or replicate the same settings.

**Review:** A tester can follow the guide and run Ditto on Synology (or in a Linux VM simulating NAS); UI loads, a scan root under the mounted volume can be added and scanned.

---

## Step 4: README and first-run experience

**What:** Top-level README that describes the project, how to run (local vs Docker), and points to Synology and release docs. Ensure first-time users see a clear path.

**Details:**
- **README sections:** What Ditto does (duplicate finder, web UI). Quick start: local (go run, or binary + data dir + port) and Docker (build/run one-liner or link to Docker section). Configuration (env vars). Links to `docs/synology.md` and `docs/release.md` (or equivalent).
- **Docker quick start:** Copy-paste `docker run` with placeholders for version and port; note that scan roots must be container paths and show a typical volume mount for "folder to scan".
- **License and repo:** License file; link to repo and any issue tracker.

**Deliverables:**
- Updated `README.md`: project summary, quick start (local + Docker), config, links to Synology and release docs.
- Optional: `CHANGELOG.md` or a "Releases" section listing versions and notable changes (can start with v0.1.0).

**Review:** New user can clone the repo, read README, and run the app locally or via Docker; user targeting Synology finds the Synology guide and succeeds.

---

## Release 1, Phase 6 done when

- [ ] Dockerfile builds and runs; data persists across container restarts.
- [ ] Image can be built for linux/amd64 and linux/arm64 (or one platform documented).
- [ ] Release process is documented (version, build, tag, push).
- [ ] Synology deployment guide is written and tested (or tested in a representative environment).
- [ ] README gives a clear path for local and Docker use and points to Synology and release docs.
- [ ] A v0.1.0 (or first release) can be cut following the release doc.

---

## Out of scope (for later)

- SPK package (ADR-001 defers to later).
- CI/CD automation (e.g. GitHub Actions to build and push on tag); can be added in an improvement.
- Automated tests in Docker (optional; manual verification is enough for phase 6).
- HTTPS / reverse proxy (user can put a proxy in front; app stays HTTP for v0.1).
