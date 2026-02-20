-- Enable slow-query logging with duration in the log line.
-- Runs once when the Postgres data volume is first created (e.g. docker compose up with a new volume).
-- Same as docker/postgres-slow-query-logging.sql; see docs/operations/postgres-slow-query-logging.md.
ALTER SYSTEM SET log_min_duration_statement = '1000';
ALTER SYSTEM SET log_line_prefix = '%t [%p] %u@%d ';
SELECT pg_reload_conf();
