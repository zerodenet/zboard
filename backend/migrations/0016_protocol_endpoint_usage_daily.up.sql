CREATE TABLE protocol_endpoint_usage_daily (
 protocol_endpoint_id bigint unsigned NOT NULL,
 usage_date date NOT NULL,
 raw_bytes bigint NOT NULL DEFAULT 0,
 upload_bytes bigint NOT NULL DEFAULT 0,
 download_bytes bigint NOT NULL DEFAULT 0,
 used_bytes bigint NOT NULL DEFAULT 0,
 record_count bigint unsigned NOT NULL DEFAULT 0,
 last_record_at datetime(3) NOT NULL,
 updated_at datetime(3) NOT NULL,
 PRIMARY KEY (protocol_endpoint_id, usage_date),
 KEY idx_protocol_endpoint_usage_date (usage_date, protocol_endpoint_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
ALTER TABLE flow_usages
 ADD KEY idx_flow_usages_endpoint_active(protocol_endpoint_id, status, last_seen_at);
ALTER TABLE protocol_deployments
 ADD KEY idx_protocol_deployments_endpoint_latest(protocol_endpoint_id, id);
INSERT INTO protocol_endpoint_usage_daily(
 protocol_endpoint_id, usage_date, raw_bytes, upload_bytes, download_bytes,
 used_bytes, record_count, last_record_at, updated_at
)
SELECT protocol_endpoint_id, DATE(record_at), COALESCE(SUM(raw_bytes), 0),
 COALESCE(SUM(upload_bytes), 0), COALESCE(SUM(download_bytes), 0),
 COALESCE(SUM(used_bytes), 0), COUNT(*), MAX(record_at), UTC_TIMESTAMP(3)
FROM traffic_records
WHERE protocol_endpoint_id > 0
GROUP BY protocol_endpoint_id, DATE(record_at);
