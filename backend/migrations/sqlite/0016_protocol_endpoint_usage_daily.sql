CREATE TABLE IF NOT EXISTS protocol_endpoint_usage_daily (
 protocol_endpoint_id INTEGER NOT NULL,
 usage_date DATE NOT NULL,
 raw_bytes INTEGER NOT NULL DEFAULT 0,
 upload_bytes INTEGER NOT NULL DEFAULT 0,
 download_bytes INTEGER NOT NULL DEFAULT 0,
 used_bytes INTEGER NOT NULL DEFAULT 0,
 record_count INTEGER NOT NULL DEFAULT 0,
 last_record_at DATETIME NOT NULL,
 updated_at DATETIME NOT NULL,
 PRIMARY KEY (protocol_endpoint_id, usage_date)
);
CREATE INDEX IF NOT EXISTS idx_protocol_endpoint_usage_date
 ON protocol_endpoint_usage_daily(usage_date, protocol_endpoint_id);
CREATE INDEX IF NOT EXISTS idx_flow_usages_endpoint_active
 ON flow_usages(protocol_endpoint_id, status, last_seen_at);
CREATE INDEX IF NOT EXISTS idx_protocol_deployments_endpoint_latest
 ON protocol_deployments(protocol_endpoint_id, id);
INSERT INTO protocol_endpoint_usage_daily(
 protocol_endpoint_id, usage_date, raw_bytes, upload_bytes, download_bytes,
 used_bytes, record_count, last_record_at, updated_at
)
SELECT protocol_endpoint_id, DATE(record_at), COALESCE(SUM(raw_bytes), 0),
 COALESCE(SUM(upload_bytes), 0), COALESCE(SUM(download_bytes), 0),
 COALESCE(SUM(used_bytes), 0), COUNT(*), MAX(record_at), CURRENT_TIMESTAMP
FROM traffic_records
WHERE protocol_endpoint_id > 0
GROUP BY protocol_endpoint_id, DATE(record_at)
ON CONFLICT(protocol_endpoint_id, usage_date) DO UPDATE SET
 raw_bytes = excluded.raw_bytes,
 upload_bytes = excluded.upload_bytes,
 download_bytes = excluded.download_bytes,
 used_bytes = excluded.used_bytes,
 record_count = excluded.record_count,
 last_record_at = excluded.last_record_at,
 updated_at = excluded.updated_at;
