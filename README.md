# Ditto

Duplicate file finder with a web UI. Scan folders, hash files, and browse duplicate groups by content (hash) or by inode (hardlinks). Designed to run on a Synology NAS via Docker.

## Quick start

### Local (Go)

```bash
# Clone and run
go run ./cmd/ditto
# Web UI at http://localhost:8080
```

Data is stored in `./data` by default. To use a different directory or port:

```bash
DITTO_DATA_DIR=/path/to/data DITTO_PORT=3000 go run ./cmd/ditto
```

### Docker

Images are published to [GitHub Container Registry](https://github.com/eargollo/ditto/pkgs/container/ditto). Use the latest release or `latest`:

```bash
# Pull and run (use a version tag for production, e.g. ghcr.io/eargollo/ditto:v0.1.0)
docker run --rm -v ditto-data:/data -p 8080:8080 ghcr.io/eargollo/ditto:latest
# Web UI at http://localhost:8080
```

Scan roots you add in the UI must be paths **inside** the container. To scan a host folder, mount it and use the container path as the scan root:

```bash
docker run --rm \
  -v ditto-data:/data \
  -v /path/on/host/Photos:/scan/Photos:ro \
  -p 8080:8080 \
  ghcr.io/eargollo/ditto:latest
# In the UI, add scan root: /scan/Photos
```

To build from source instead: `docker build -t ditto .` then use the `ditto` image in the commands above.

### Docker Compose

See [docker-compose.yml](docker-compose.yml). Set the image to `ghcr.io/eargollo/ditto:latest` (or a version tag), uncomment and set the volume for the folder(s) you want to scan, then:

```bash
docker compose up -d
```

## Running on Synology

1. **Install Container Manager** (Package Center) if needed.
2. **Get your user UID/GID** — SSH into the NAS and run `id admin` (or your user). Example: `uid=1026(admin) gid=100(users)` → use `PUID=1026`, `PGID=100`.
3. **Create a folder** for Ditto data (e.g. `Docker/ditto/data`) in File Station. It will be owned by your user.
4. **Create a container** in Container Manager:
   - **Image:** `ghcr.io/eargollo/ditto:latest` (or a version tag like `ghcr.io/eargollo/ditto:v0.1.0`).
   - **Environment:** `DITTO_DATA_DIR=/data`, `DITTO_PORT=8080`, `PUID=<your UID>`, `PGID=<your GID>`.
   - **Volume:** mount your folder → container path `/data` (e.g. `Docker/ditto/data` → `/data`).
   - **Optional — folders to scan:** mount shared folders so they appear inside the container (e.g. `Photos` → `/scan/Photos`). In the UI, add scan root **`/scan/Photos`** (the path inside the container).
   - **Port:** map container port 8080 to a host port (e.g. 8080 or 32480).
5. **Start the container** and open **`http://<NAS-IP>:<host-port>`** in a browser.

**Compose example (Synology):** use host paths and set `PUID`/`PGID` to your DSM user (from `id admin`). Replace `/volume1/...` with your NAS paths.

```yaml
services:
  ditto:
    image: ghcr.io/eargollo/ditto:latest
    container_name: ditto
    restart: unless-stopped
    environment:
      DITTO_DATA_DIR: /data
      DITTO_PORT: 8080
      PUID: 1026   # your UID from "id admin"
      PGID: 100    # your GID
    volumes:
      - /volume1/docker/ditto/data:/data
      - /volume1/Photos:/scan/Photos:ro
    ports:
      - "8080:8080"
```

In the UI, add scan root **`/scan/Photos`**. For more detail (permissions, troubleshooting), see **[Running on Synology](docs/synology.md)**.

## Configuration

| Variable           | Default   | Description                    |
|-------------------|-----------|--------------------------------|
| `DITTO_DATA_DIR`  | `./data`  | Directory for SQLite DB and data. |
| `DITTO_PORT`      | `8080`    | HTTP port for the web UI.     |
| `PUID` / `PGID`   | `1000` / `1000` | (Docker only) Run the app as this user. On Synology, set to your DSM user's UID/GID when you mount a **host folder** for `/data` so the app can write to it. Use `id youruser` on the NAS to get the values. |

## Documentation

- **[Running on Synology](docs/synology.md)** — Deploy Ditto on a Synology NAS with Container Manager (Docker): image, volumes, env, and scan roots.
- **[Release process](docs/release.md)** — How to build and publish the Docker image and optional binaries (versioning, multi-platform build, git tags).

## Development

Tests and the app require PostgreSQL. Start the dev database, then run tests or the app:

```bash
docker compose -f docker-compose.dev.yml up -d
make test   # or: go test -p 1 ./...
export DATABASE_URL="postgres://ditto:ditto@localhost:5432/ditto?sslmode=disable"
make build  # or: go build -o ditto ./cmd/ditto
go run ./cmd/ditto   # or run ./ditto
```

**Important:** Tests must run with `-p 1` (one package at a time) because they share a single Postgres instance and truncate the same tables; running `go test ./...` without `-p 1` causes deadlocks. Use `make test` to get the correct flags.

Tests default to `postgres://ditto:ditto@localhost:5432/ditto?sslmode=disable` when `DATABASE_URL` is unset. The app still requires `DATABASE_URL`. See [docker-compose.dev.yml](docker-compose.dev.yml) for credentials and port.

## License

See [LICENSE](LICENSE) if present.
