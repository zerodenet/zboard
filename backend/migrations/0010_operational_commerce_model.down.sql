DROP TABLE quota_events;
DROP TABLE protocol_deployments;
DROP TABLE system_configs;
DROP TABLE task_items;
DROP TABLE tasks;
DROP TABLE user_api_tokens;
DROP TABLE subscription_members;
DROP TABLE payment_events;

ALTER TABLE orders
  DROP FOREIGN KEY fk_orders_target_subscription,
  DROP INDEX uk_orders_provider_trade_no,
  DROP INDEX idx_orders_target_subscription,
  DROP COLUMN failure_reason,
  DROP COLUMN refunded_at,
  DROP COLUMN fulfilled_at,
  DROP COLUMN canceled_at,
  DROP COLUMN paid_at,
  DROP COLUMN provider_trade_no,
  DROP COLUMN discount_amount,
  DROP COLUMN refund_amount,
  DROP COLUMN paid_amount,
  DROP COLUMN payable_amount,
  DROP COLUMN target_subscription_id,
  DROP COLUMN order_type;

ALTER TABLE traffic_records
  DROP COLUMN updated_at,
  DROP COLUMN created_at,
  DROP COLUMN subscription_multiplier_milli,
  DROP COLUMN protocol_multiplier_milli,
  DROP COLUMN traffic_calc_mode,
  DROP COLUMN download_bytes,
  DROP COLUMN upload_bytes;

ALTER TABLE subscription_tokens
  DROP FOREIGN KEY fk_subscription_tokens_subscription,
  DROP INDEX idx_subscription_tokens_subscription,
  DROP COLUMN subscription_id;

ALTER TABLE subscriptions
  DROP FOREIGN KEY fk_subscriptions_access_group,
  DROP INDEX idx_subscriptions_reset,
  DROP INDEX idx_subscriptions_access_group,
  DROP COLUMN config,
  DROP COLUMN traffic_multiplier_milli,
  DROP COLUMN traffic_calc_mode,
  DROP COLUMN next_reset_at,
  DROP COLUMN reset_policy,
  DROP COLUMN renewal_price_minor,
  DROP COLUMN family_limit,
  DROP COLUMN device_limit,
  DROP COLUMN speed_limit_mbps,
  DROP COLUMN subscription_type,
  DROP COLUMN access_group_id;

ALTER TABLE plan_skus DROP COLUMN sku_type;
ALTER TABLE plans
  DROP FOREIGN KEY fk_plans_access_group,
  DROP INDEX idx_plans_access_group,
  DROP COLUMN traffic_multiplier_milli,
  DROP COLUMN traffic_calc_mode,
  DROP COLUMN reset_policy,
  DROP COLUMN family_limit,
  DROP COLUMN device_limit,
  DROP COLUMN is_renewable,
  DROP COLUMN max_active_subscriptions,
  DROP COLUMN speed_limit_mbps,
  DROP COLUMN traffic_bytes,
  DROP COLUMN access_group_id;
DROP TABLE access_group_protocols;
DROP TABLE access_groups;

ALTER TABLE protocol_endpoints
  DROP FOREIGN KEY fk_protocol_parent,
  DROP INDEX idx_protocol_parent,
  DROP COLUMN tags,
  DROP COLUMN optional_config,
  DROP COLUMN parent_protocol_id,
  DROP COLUMN cipher,
  DROP COLUMN public_port;

ALTER TABLE nodes
  DROP COLUMN version,
  DROP COLUMN last_sync_at,
  DROP COLUMN remark,
  DROP COLUMN is_enabled,
  DROP COLUMN config,
  DROP COLUMN status,
  DROP COLUMN communication_protocol,
  DROP COLUMN node_credential;

ALTER TABLE users
  DROP COLUMN last_login_at,
  DROP COLUMN email_verified_at,
  DROP COLUMN account_type,
  DROP COLUMN account_name;
