# Running Ditto on Synology NAS

This guide explains how to run Ditto in Synology Container Manager (Docker) so you can scan shared folders on your NAS for duplicate files.

## Prerequisites

- Synology NAS with **Container Manager** (or Docker) installed (Package Center → Container Manager).
- One or more shared folders you want to scan (e.g. `Photos`, `Documents`).

## Pull the image

The image is published to **GitHub Container Registry**. Use `ghcr.io/eargollo/ditto:latest` or a version tag (e.g. `ghcr.io/eargollo/ditto:v0.1.0`).

In Container Manager:

1. Open **Registry** → search for `eargollo/ditto` or add the GitHub Container Registry URL if needed.
2. Download the image. Choose the tag that matches your NAS (the image is multi-platform):
   - **x64 NAS:** `linux/amd64`
   - **ARM NAS (e.g. DS223j, DS224+):** `linux/arm64`

Or via SSH: `docker pull ghcr.io/eargollo/ditto:latest`

## Create the container

1. In Container Manager, go to **Project** (or **Container**) → **Create** → **Create with default settings** (or **Create project** and add a container).
2. **Image:** Select the Ditto image and tag you downloaded.
3. **General settings:**
   - Container name: e.g. `ditto`
   - Enable **Auto-restart** if you want Ditto to start after NAS reboot.

### Environment variables

Add the following:

| Variable          | Value   | Description                          |
|-------------------|---------|--------------------------------------|
| `DITTO_PORT`      | `8080`  | Port the app listens on inside the container. |
| `DATABASE_URL`    | (required) | PostgreSQL connection URL. All Ditto state (folders, scans, files) is stored in Postgres. |
| `DITTO_DATA_DIR`  | (optional) | Local app data directory; default `./data`. Not used for the database. You can omit it in Docker. |
| `PUID` / `PGID`   | (optional) | Override container user when needed for volume permissions (e.g. scan folders). |

**Optional – scan pipeline (for NAS / low-resource):**  
On Synology, the default scan pipeline (4 walkers, 2 writers) may use more CPU and memory than you want. You can tune it with:

| Variable                 | Recommended (Synology) | Description |
|--------------------------|-------------------------|-------------|
| `DITTO_SCAN_WALKERS`     | `2`                     | Goroutines that list directories (default 4). |
| `DITTO_SCAN_WRITERS`     | `1`                     | Goroutines that batch-write to the DB (default 2). |
| `DITTO_SCAN_BATCH_SIZE`  | `250`                   | Max files per DB batch (default 500). |
| `DITTO_SCAN_FILE_CHAN_CAP` | `500`                 | File channel buffer size (default 1000). |

Example: set `DITTO_SCAN_WALKERS=2`, `DITTO_SCAN_WRITERS=1`, `DITTO_SCAN_BATCH_SIZE=250` in the container environment for a lighter scan load.

**Finding your UID/GID on Synology:** SSH into the NAS and run `id` (e.g. `id admin`). Use `PUID`/`PGID` if you need the container to match your user for volume permissions.

### Volumes

1. **Postgres data (required if Postgres runs in a container)**  
   Mount Postgres data out so the database survives container recreation. In the compose example this is the `ditto-postgres-data` volume mounted at `/var/lib/postgresql/data` in the postgres service. Without it, recreating the Postgres container wipes the database.

2. **Folders to scan (one or more)**  
   Mount each shared folder you want to scan so it appears inside the container.  
   - **File/Folder:** e.g. `volume1/Photos` (browse to your shared folder).  
   - **Mount path:** e.g. `/scan/Photos` (path as seen inside the container).  
   - You can add more: e.g. `volume1/Documents` → `/scan/Documents`.  
   - **Read-only** is recommended so the app only reads files and does not modify them.

### Port mapping

- **Local port:** Choose a host port (e.g. `8080` or `32480`) that is not used by other services.  
- **Container port:** `8080` (must match `DITTO_PORT`).

### Permissions

The image runs as a non-root user (UID 1000). If the container cannot read your shared folders:

- In **Control Panel → Shared Folder**, ensure the folder has **Read** (or Read/Write) for the user that runs the container, or use “Everyone” with read access for testing.
- Some guides suggest setting the container’s user to match your NAS (e.g. UID/GID of `admin`). You can override the user in the container advanced settings if needed.

## Start and open the UI

1. Start the container (or project).
2. On your computer or phone, open a browser and go to:
   ```text
   http://<NAS-IP>:<HOST_PORT>
   ```
   Example: `http://192.168.1.100:8080`
3. You should see the Ditto UI (Scans, add scan root, etc.).

## Adding scan roots in the UI

Scan roots are **paths inside the container**, not NAS paths.

- If you mounted `volume1/Photos` at `/scan/Photos`, add scan root: **`/scan/Photos`**.
- If you mounted `volume1/Documents` at `/scan/Documents`, add **`/scan/Documents`**.

Then use **Start scan** for each root. Scans run one at a time; you can queue multiple folders.

## Example with docker-compose

If you use `docker-compose` (e.g. via Container Manager’s Compose support or SSH), you can use this as a template. **Important:** Ditto stores all data (folders, scans, file list) in PostgreSQL. To avoid losing data when you recreate containers, Postgres must use a **persistent volume** for its data directory (`/var/lib/postgresql/data`). The example below includes a `postgres` service with a named volume so data survives container recreation. Adjust ports and volume paths to match your NAS:

```yaml
services:
  postgres:
    image: postgres:18-alpine
    container_name: ditto-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: ditto
      POSTGRES_PASSWORD: ditto
      POSTGRES_DB: ditto
      PGDATA: /var/lib/postgresql/data/pgdata 
    volumes:
      - ditto-postgres-data:/var/lib/postgresql/data

  ditto:
    image: ghcr.io/eargollo/ditto:latest
    container_name: ditto
    restart: unless-stopped
    depends_on:
      - postgres
    environment:
      DITTO_PORT: 8080
      DATABASE_URL: postgres://ditto:ditto@postgres:5432/ditto?sslmode=disable
    volumes:
      - /volume1/Photos:/scan/Photos:ro
    ports:
      - "8080:8080"

volumes:
  ditto-postgres-data:
```

Replace `/volume1/Photos` with the host path to the folder you want to scan; use `/scan/Photos` (or similar) as the scan root in the UI.

If you use an **external** Postgres (not in this compose), ensure its data directory is on a persistent volume; otherwise recreating that Postgres container will wipe the database.

## Troubleshooting

- **Database empty every time I recreate the container:** All state is in **PostgreSQL**. If Postgres runs in a container, you must mount its data out: use a persistent volume for `/var/lib/postgresql/data` (e.g. `ditto-postgres-data` in the compose example). Without it, recreating the Postgres container wipes the database.
- **Cannot add scan root / permission denied:** Ensure the shared folder is readable by the user the container runs as; check volume mount path and permissions in DSM.
- **Container exits immediately:** Check **Log** in Container Manager for errors (e.g. database connection, invalid DATABASE_URL).
- **UI not reachable:** Confirm the container is running and the host port is correct; check firewall or DSM firewall rules if needed.
