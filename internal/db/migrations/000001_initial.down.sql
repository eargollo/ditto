-- Down migration: drop tables in reverse dependency order.
-- Use only when intentionally rolling back schema; data will be lost.

DROP TABLE IF EXISTS duplicate_groups_hash;
DROP TABLE IF EXISTS file_scan;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS scans;
DROP TABLE IF EXISTS folders;
