# Postgres slow-query logging (duration in log line)

Postgres can log every statement that runs longer than a threshold; each log line includes **duration in milliseconds**.

## When to apply

Run the SQL setup **when you create or recreate the Postgres instance**, for example:

- New Docker volume (e.g. after `docker compose down -v` and `up` again)
- New server or new Postgres container that doesn’t use the compose `command` override
- Synology or any deployment where you didn’t set `log_min_duration_statement` via `command`

If you use `docker-compose.dev.yml` or `docker-compose.yml` with the `command` that sets these options, the settings are already applied when the container starts. You only need the SQL when you’re **not** using that (e.g. plain Postgres image with no `command`) or when you’ve recreated the data volume and want to rely on persistent config instead of compose.

## How to apply

```bash
psql "$DATABASE_URL" -f docker/postgres-slow-query-logging.sql
```

Requires a superuser connection (e.g. the `ditto` user when it’s the only user and owns the DB). Settings are written to `postgresql.auto.conf` and survive restarts.

## What it sets

| Setting | Value | Meaning |
|--------|--------|--------|
| `log_min_duration_statement` | 1000 | Log any statement that runs ≥ 1 second (value in ms) |
| `log_line_prefix` | `%t [%p] %u@%d ` | Each log line starts with timestamp, PID, user, database |

Logged lines look like:

```
2026-02-20 12:00:01.234 UTC [12345] ditto@ditto LOG:  duration: 5432.123 ms  statement: UPDATE files SET ...
```

## Changing the threshold

To log statements slower than 500 ms instead of 1 s:

```sql
ALTER SYSTEM SET log_min_duration_statement = '500';
SELECT pg_reload_conf();
```

Use `'0'` to log every statement (noisy).

## Automatic apply on new volume (dev)

The same settings are applied automatically when the **dev** Postgres volume is first created: see `docker/postgres-init/02-slow-query-logging.sql`. So if you use `docker compose -f docker-compose.dev.yml` and create a new volume, you don’t need to run the SQL manually. For other setups (e.g. production, Synology), run `docker/postgres-slow-query-logging.sql` after creating or recreating the instance.
