ALTER TABLE `traffic_records`
  DROP INDEX `idx_traffic_records_subscription_created_at`;

ALTER TABLE `subscriptions`
  DROP COLUMN `cycle_start_used`,
  DROP COLUMN `reset_quota_bytes`;
