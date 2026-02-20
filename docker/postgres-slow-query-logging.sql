-- Enable Postgres slow-query logging with duration in the log line.
-- Run this when you create or recreate the Postgres instance (e.g. new volume, new server)
-- so that statements exceeding the threshold are logged with "duration: X.XXX ms".
--
-- Apply with (use your DATABASE_URL):
--   psql "$DATABASE_URL" -f docker/postgres-slow-query-logging.sql
--
-- Settings are stored in postgresql.auto.conf and survive restarts.
-- To change the threshold later: ALTER SYSTEM SET log_min_duration_statement = '500';  -- 500 ms
-- Then: SELECT pg_reload_conf();

ALTER SYSTEM SET log_min_duration_statement = '1000';
ALTER SYSTEM SET log_line_prefix = '%t [%p] %u@%d ';
SELECT pg_reload_conf();
