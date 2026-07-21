ALTER TABLE traffic_records
  ADD COLUMN subscription_id BIGINT UNSIGNED NULL AFTER user_id,
  ADD INDEX idx_traffic_records_subscription_id (subscription_id);
