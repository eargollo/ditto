# Release process

This document describes how to cut a release of Ditto (Docker image and optional standalone binaries).

## Prerequisites

- Go 1.24+ (or version in `go.mod`)
- Docker with buildx (for multi-platform images)
- Access to push to your container registry (Docker Hub, GHCR, etc.)

## Version and git tag

1. Choose a version (e.g. `v0.1.0`). Use [semantic versioning](https://semver.org/).
2. Ensure the working tree is clean and tests pass:
   ```bash
   go test ./...
   ```
3. Create and push an annotated tag:
   ```bash
   git tag -a v0.1.0 -m "Release v0.1.0"
   git push origin v0.1.0
   ```

## Build Docker image

### Single platform (local testing)

```bash
docker build -t ditto:v0.1.0 .
docker run --rm -v ditto-data:/data -p 8080:8080 ditto:v0.1.0
```

### Multi-platform (Synology: amd64 and arm64)

Build for both linux/amd64 and linux/arm64 so the image runs on x64 and ARM Synology NAS:

```bash
docker buildx create --use --name ditto-builder  # one-time
docker buildx build --platform linux/amd64,linux/arm64 \
  -t YOUR_REGISTRY/ditto:v0.1.0 \
  --push .
```

Replace `YOUR_REGISTRY` with your Docker Hub username (e.g. `eargollo/ditto`) or GHCR path (e.g. `ghcr.io/eargollo/ditto`).

To build without pushing (load into local Docker for the current platform):

```bash
docker buildx build --platform linux/amd64 -t ditto:v0.1.0 --load .
```

## Optional: standalone binaries

For users who prefer not to use Docker:

```bash
# Linux amd64
GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o dist/ditto-linux-amd64 ./cmd/ditto

# Linux arm64 (Synology ARM NAS)
GOOS=linux GOARCH=arm64 go build -ldflags="-w -s" -o dist/ditto-linux-arm64 ./cmd/ditto

# macOS (local dev)
GOOS=darwin GOARCH=amd64 go build -ldflags="-w -s" -o dist/ditto-darwin-amd64 ./cmd/ditto
GOOS=darwin GOARCH=arm64 go build -ldflags="-w -s" -o dist/ditto-darwin-arm64 ./cmd/ditto
```

Run with:

```bash
DITTO_DATA_DIR=./data DITTO_PORT=8080 ./dist/ditto-linux-amd64
```

## Publish checklist

- [ ] Tests pass: `go test ./...`
- [ ] Git tag created and pushed (e.g. `v0.1.0`)
- [ ] Docker image built for target platform(s) and pushed to registry
- [ ] (Optional) Binaries built and attached to GitHub Release or published
- [ ] Release notes or CHANGELOG updated

## Version in the app (optional)

To show the version in the UI or `/health`, inject at build time:

```bash
go build -ldflags="-X main.Version=v0.1.0 -w -s" -o ditto ./cmd/ditto
```

Then in `cmd/ditto/main.go` define `var Version = "dev"` and use it in the server or health handler. Phase 6 does not require this; it can be added in a later refinement.
