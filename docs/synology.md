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
| `DITTO_DATA_DIR`  | `/data` | Where the database and data are stored (use a mounted volume). |
| `DITTO_PORT`      | `8080`  | Port the app listens on inside the container. |
| `PUID`            | (optional) | Your Synology user's UID. Set this (and `PGID`) when you mount a **host folder** for `/data` so the app runs as your user and can write without permission errors. Omit or leave default (1000) for a Docker **named** volume. |
| `PGID`            | (optional) | Your Synology user's GID. Use together with `PUID`. |

**Finding your UID/GID on Synology:** SSH into the NAS and run `id` (e.g. `id admin`). You'll see e.g. `uid=1026(admin) gid=100(users)` — use `PUID=1026` and `PGID=100`. Then create a folder for Ditto data (e.g. `Docker/ditto/data`) owned by that user; the container will write to it when you mount it at `/data`.

### Volumes (port settings)

1. **Data volume (required)**  
   - **File/Folder:** Create or select a folder, e.g. `Docker/ditto/data` (so data survives container recreation).  
   - **Mount path:** `/data`  
   - This stores the SQLite database and any app data.

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

If you use `docker-compose` (e.g. via Container Manager’s Compose support or SSH), you can use this as a template. Adjust ports and volume paths to match your NAS:

```yaml
services:
  ditto:
    image: ghcr.io/eargollo/ditto:latest
    container_name: ditto
    restart: unless-stopped
    environment:
      DITTO_DATA_DIR: /data
      DITTO_PORT: 8080
      # PUID: 1026
      # PGID: 100
    volumes:
      - /volume1/docker/ditto/data:/data
      - /volume1/Photos:/scan/Photos:ro
    ports:
      - "8080:8080"
```

Replace:

- `/volume1/docker/ditto/data` with a path where you want to persist data (set `PUID`/`PGID` to your user if needed).
- `/volume1/Photos` with the host path to the folder you want to scan; use `/scan/Photos` (or similar) as the scan root in the UI.

## Troubleshooting

- **Cannot add scan root / permission denied:** Ensure the shared folder is readable by the user the container runs as; check volume mount path and permissions in DSM.
- **Container exits immediately:** Check **Log** in Container Manager for errors (e.g. missing `/data` or permission issues). Ensure the data volume is mounted at `/data`.
- **UI not reachable:** Confirm the container is running and the host port is correct; check firewall or DSM firewall rules if needed.
