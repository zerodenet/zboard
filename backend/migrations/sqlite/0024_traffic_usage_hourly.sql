CREATE TABLE IF NOT EXISTS traffic_usage_hourly (
 record_at TEXT NOT NULL,
 user_id BIGINT NOT NULL DEFAULT 0,
 subscription_id BIGINT NOT NULL DEFAULT 0,
 node_id BIGINT NOT NULL DEFAULT 0,
 protocol_endpoint_id BIGINT NOT NULL DEFAULT 0,
 protocol_multiplier_milli BIGINT NOT NULL DEFAULT 0,
 raw_bytes BIGINT NOT NULL DEFAULT 0,
 upload_bytes BIGINT NOT NULL DEFAULT 0,
 download_bytes BIGINT NOT NULL DEFAULT 0,
 used_bytes BIGINT NOT NULL DEFAULT 0,
 record_count BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY (record_at, user_id, subscription_id, node_id, protocol_endpoint_id, protocol_multiplier_milli)
);
CREATE INDEX IF NOT EXISTS idx_traffic_hourly_user_id ON traffic_usage_hourly(user_id, record_at);
CREATE INDEX IF NOT EXISTS idx_traffic_hourly_subscription_id ON traffic_usage_hourly(subscription_id, record_at);
CREATE INDEX IF NOT EXISTS idx_traffic_hourly_node_id ON traffic_usage_hourly(node_id, record_at);
CREATE INDEX IF NOT EXISTS idx_traffic_hourly_protocol_endpoint_id ON traffic_usage_hourly(protocol_endpoint_id, record_at);
INSERT INTO traffic_usage_hourly(record_at, user_id, subscription_id, node_id, protocol_endpoint_id, protocol_multiplier_milli, raw_bytes, upload_bytes, download_bytes, used_bytes, record_count)
SELECT strftime('%Y-%m-%d %H:00:00', record_at), COALESCE(user_id, 0), COALESCE(subscription_id, 0), COALESCE(node_id, 0), COALESCE(protocol_endpoint_id, 0), COALESCE(protocol_multiplier_milli, 0), COALESCE(SUM(raw_bytes), 0), COALESCE(SUM(upload_bytes), 0), COALESCE(SUM(download_bytes), 0), COALESCE(SUM(used_bytes), 0), COUNT(*)
FROM traffic_records GROUP BY strftime('%Y-%m-%d %H:00:00', record_at), COALESCE(user_id, 0), COALESCE(subscription_id, 0), COALESCE(node_id, 0), COALESCE(protocol_endpoint_id, 0), COALESCE(protocol_multiplier_milli, 0);
