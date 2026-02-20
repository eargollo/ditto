-- Speeds up deleted_at "not in scan" update: NOT EXISTS probe and "in scan" JOIN use (scan_id, file_id).
CREATE INDEX IF NOT EXISTS idx_file_scan_scan_id_file_id ON file_scan(scan_id, file_id);