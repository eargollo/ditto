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

```bash
# Build and run
docker build -t ditto .
docker run --rm -v ditto-data:/data -p 8080:8080 ditto
# Web UI at http://localhost:8080
```

Scan roots you add in the UI must be paths **inside** the container. To scan a host folder, mount it and use the container path as the scan root:

```bash
docker run --rm \
  -v ditto-data:/data \
  -v /path/on/host/Photos:/scan/Photos:ro \
  -p 8080:8080 \
  ditto
# In the UI, add scan root: /scan/Photos
```

### Docker Compose

See [docker-compose.yml](docker-compose.yml). Uncomment and set the volume for the folder(s) you want to scan, then:

```bash
docker compose up -d
```

## Running on Synology

1. **Install Container Manager** (Package Center) if needed.
2. **Get your user UID/GID** — SSH into the NAS and run `id admin` (or your user). Example: `uid=1026(admin) gid=100(users)` → use `PUID=1026`, `PGID=100`.
3. **Create a folder** for Ditto data (e.g. `Docker/ditto/data`) in File Station. It will be owned by your user.
4. **Create a container** in Container Manager:
   - **Image:** pull or use your built image (e.g. `ditto:latest`).
   - **Environment:** `DITTO_DATA_DIR=/data`, `DITTO_PORT=8080`, `PUID=<your UID>`, `PGID=<your GID>`.
   - **Volume:** mount your folder → container path `/data` (e.g. `Docker/ditto/data` → `/data`).
   - **Optional — folders to scan:** mount shared folders so they appear inside the container (e.g. `Photos` → `/scan/Photos`). In the UI, add scan root **`/scan/Photos`** (the path inside the container).
   - **Port:** map container port 8080 to a host port (e.g. 8080 or 32480).
5. **Start the container** and open **`http://<NAS-IP>:<host-port>`** in a browser.

For more detail (permissions, compose example, troubleshooting), see **[Running on Synology](docs/synology.md)**.

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

```bash
go test ./...
go build -o ditto ./cmd/ditto
```

## License

See [LICENSE](LICENSE) if present.
