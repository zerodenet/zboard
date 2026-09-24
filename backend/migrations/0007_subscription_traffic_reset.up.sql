ALTER TABLE `subscriptions`
  ADD COLUMN `reset_quota_bytes` bigint NOT NULL DEFAULT 0,
  ADD COLUMN `cycle_start_used` bigint NOT NULL DEFAULT 0;

ALTER TABLE `traffic_records`
  ADD INDEX `idx_traffic_records_subscription_created_at` (`subscription_id`, `created_at`);

UPDATE `subscriptions` AS s
LEFT JOIN `plans` AS p ON p.id = s.plan_id
SET s.reset_quota_bytes = COALESCE(
  (SELECT o.traffic_bytes FROM `orders` AS o
   WHERE o.subscription_id = s.id AND o.status = 'paid' AND o.order_type <> 'traffic_pack' AND o.traffic_bytes > 0
   ORDER BY o.fulfilled_at DESC, o.id DESC LIMIT 1),
  p.traffic_bytes, 0)
WHERE s.reset_policy BETWEEN 1 AND 4;
