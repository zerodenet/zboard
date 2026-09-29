-- Replaces the privileged MySQL triggers from 0024. The migration runner
-- reconciles existing indexes/triggers before rebuilding the derived table
-- and recording both versions in one DML transaction. Application writers
-- maintain this projection in the same transaction as the immutable ledger.
CREATE TABLE IF NOT EXISTS traffic_usage_hourly (
  record_at DATETIME NOT NULL,
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
  PRIMARY KEY (record_at, user_id, subscription_id, node_id, protocol_endpoint_id, protocol_multiplier_milli),
  KEY idx_traffic_hourly_user_id (user_id, record_at),
  KEY idx_traffic_hourly_subscription_id (subscription_id, record_at),
  KEY idx_traffic_hourly_node_id (node_id, record_at),
  KEY idx_traffic_hourly_protocol_endpoint_id (protocol_endpoint_id, record_at)
) ENGINE=InnoDB;

DELETE FROM traffic_usage_hourly;
INSERT INTO traffic_usage_hourly (record_at, user_id, subscription_id, node_id, protocol_endpoint_id, protocol_multiplier_milli, raw_bytes, upload_bytes, download_bytes, used_bytes, record_count)
SELECT CAST(DATE_FORMAT(record_at, '%Y-%m-%d %H:00:00') AS DATETIME), COALESCE(user_id, 0), COALESCE(subscription_id, 0), COALESCE(node_id, 0), COALESCE(protocol_endpoint_id, 0), COALESCE(protocol_multiplier_milli, 0), COALESCE(SUM(raw_bytes), 0), COALESCE(SUM(upload_bytes), 0), COALESCE(SUM(download_bytes), 0), COALESCE(SUM(used_bytes), 0), COUNT(*)
FROM traffic_records
GROUP BY CAST(DATE_FORMAT(record_at, '%Y-%m-%d %H:00:00') AS DATETIME), COALESCE(user_id, 0), COALESCE(subscription_id, 0), COALESCE(node_id, 0), COALESCE(protocol_endpoint_id, 0), COALESCE(protocol_multiplier_milli, 0);
