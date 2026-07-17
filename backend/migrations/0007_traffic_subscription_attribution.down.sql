ALTER TABLE traffic_records
  DROP INDEX idx_traffic_records_subscription_id,
  DROP COLUMN subscription_id;
