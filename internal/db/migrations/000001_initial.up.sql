-- Initial schema for Ditto (from scratch).
-- Tables: folders, scans, files, file_scan, duplicate_groups_hash.
-- Timestamps: timestamptz, UTC.

CREATE TABLE IF NOT EXISTS folders (
	id BIGSERIAL PRIMARY KEY,
	path TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

CREATE TABLE IF NOT EXISTS scans (
	id BIGSERIAL PRIMARY KEY,
	folder_id BIGINT NOT NULL REFERENCES folders(id),
	started_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
	completed_at TIMESTAMPTZ,
	hash_started_at TIMESTAMPTZ,
	hash_completed_at TIMESTAMPTZ,
	file_count BIGINT,
	scan_skipped_count BIGINT,
	hashed_file_count BIGINT,
	hashed_byte_count BIGINT,
	hash_reused_count BIGINT,
	hash_error_count BIGINT,
	deleted_at_update_duration_ms BIGINT
);

CREATE INDEX IF NOT EXISTS idx_scans_folder_id ON scans(folder_id);
CREATE INDEX IF NOT EXISTS idx_scans_started_at ON scans(started_at DESC);

CREATE TABLE IF NOT EXISTS files (
	id BIGSERIAL PRIMARY KEY,
	folder_id BIGINT NOT NULL REFERENCES folders(id),
	path TEXT NOT NULL,
	size BIGINT NOT NULL,
	mtime BIGINT NOT NULL,
	inode BIGINT,
	device_id BIGINT,
	hash TEXT,
	hash_status TEXT NOT NULL DEFAULT 'pending',
	hashed_at TIMESTAMPTZ,
	hashed_mtime BIGINT,
	deleted_at TIMESTAMPTZ,
	UNIQUE(folder_id, path)
);

CREATE INDEX IF NOT EXISTS idx_files_folder_id ON files(folder_id);
CREATE INDEX IF NOT EXISTS idx_files_folder_id_path ON files(folder_id, path);
CREATE INDEX IF NOT EXISTS idx_files_hash_status ON files(hash_status);
CREATE INDEX IF NOT EXISTS idx_files_hash ON files(hash) WHERE hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_files_inode_device ON files(inode, device_id);
CREATE INDEX IF NOT EXISTS idx_files_inode_device_size ON files(inode, device_id, size);

CREATE TABLE IF NOT EXISTS file_scan (
	file_id BIGINT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
	scan_id BIGINT NOT NULL REFERENCES scans(id) ON DELETE CASCADE,
	PRIMARY KEY (file_id, scan_id)
);

CREATE INDEX IF NOT EXISTS idx_file_scan_scan_id ON file_scan(scan_id);
CREATE INDEX IF NOT EXISTS idx_file_scan_file_id ON file_scan(file_id);

CREATE TABLE IF NOT EXISTS duplicate_groups_hash (
	hash TEXT PRIMARY KEY,
	file_count BIGINT NOT NULL,
	total_size BIGINT NOT NULL,
	reclaimable_size BIGINT NOT NULL
);
