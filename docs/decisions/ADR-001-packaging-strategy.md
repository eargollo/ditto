# ADR-001: Packaging strategy for Synology deployment

**Date**: 2025-02-07

## Decision

1. **Start with Docker only**
   - Ship the application as a Docker image (e.g. on Docker Hub) with instructions and optional `docker-compose.yml` for Synology Container Manager.
   - This avoids SPK and cross-compilation until the product is stable and the UX is validated.

2. **Implement the application in Golang**
   - Good performance for file I/O and hashing.
   - Single static binary per platform (e.g. linux/amd64, linux/arm64), which fits Docker and future SPK workflows.
   - No runtime dependency on the NAS (unlike Python/Node), simplifying the final image and SPK later.

3. **Evolve to Docker-in-SPK as the product matures**
   - Once the application is mature and user feedback is positive, we will add an SPK package that:
     - Declares a dependency on the Docker package.
     - Uses the Synology docker worker (e.g. `conf/resource`) to run the same Docker image.
   - This gives one-click install via Package Center and proper DSM lifecycle (start/stop, wizards) without introducing cross-compilation or changing the core delivery artifact (the same image).

## Context

Ditto is a duplicate file finder application targeting Synology NAS devices. We needed to choose how to package and distribute the application. Options considered included:

- **Native SPK** – Full DSM integration but requires cross-compilation for multiple Synology architectures and more tooling (Synology Toolkit or SynoCommunity spksrc).
- **Docker wrapped in SPK** – Package Center–installable, no cross-compilation (single Docker image), but depends on users having the Docker package installed.
- **Docker only** – Publish a container image; users run it via Synology Container Manager. No SPK tooling, fastest path to a working app.
- **Standalone binary/script** – No packaging; run via SSH or Task Scheduler. Simplest but poorest UX and no DSM integration.

We also needed to choose an implementation language suitable for performance (scanning many files) and for eventual packaging.

## Consequences

- **Positive**
  - Faster time to first working version; no SPK or Toolkit setup required initially.
  - Single codebase and one image to maintain; SPK becomes a thin wrapper later.
  - Golang keeps the image small and startup fast, and aligns with Synology’s common architectures (amd64/arm64) via standard Go cross-compilation.
- **Negative**
  - Early users must have Docker/Container Manager and follow manual setup until the SPK exists.
  - Some Synology path/permission quirks (e.g. volume mounts, UID/GID) will need to be documented for the Docker-only phase.
- **Neutral**
  - SPK work is deferred but not abandoned; the decision explicitly plans for Docker-in-SPK as the next step.
