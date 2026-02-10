#!/bin/sh
set -e
# Optional: run as Synology (or host) user so /data can be a folder owned by that user.
# Set PUID and PGID to your DSM user's UID/GID (e.g. 1026:100 for admin). Default 1000:1000.
PUID=${PUID:-1000}
PGID=${PGID:-1000}
# Create or reuse group with desired GID (Alpine may already have e.g. "users" with GID 100)
if ! getent group "$PGID" >/dev/null 2>&1; then
  addgroup -g "$PGID" ditto
fi
GROUP=$(getent group "$PGID" | cut -d: -f1)
# Create user with desired UID, or use existing user with that UID
if getent passwd "$PUID" >/dev/null 2>&1; then
  RUNAS=$(getent passwd "$PUID" | cut -d: -f1)
else
  adduser -D -u "$PUID" -G "$GROUP" ditto
  RUNAS=ditto
fi
chown -R "$PUID:$PGID" /data 2>/dev/null || true
exec su "$RUNAS" -s /bin/sh -c "exec /app/ditto"
