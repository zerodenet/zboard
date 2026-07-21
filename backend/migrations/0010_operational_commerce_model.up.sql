ALTER TABLE users
  ADD COLUMN account_name VARCHAR(80) NULL AFTER id,
  ADD COLUMN account_type TINYINT NOT NULL DEFAULT 3 AFTER password,
  ADD COLUMN email_verified_at DATETIME(3) NULL AFTER account_type,
  ADD COLUMN last_login_at DATETIME(3) NULL AFTER email_verified_at;
UPDATE users SET account_name = SUBSTRING_INDEX(email, '@', 1) WHERE account_name IS NULL OR account_name = '';

ALTER TABLE nodes
  ADD COLUMN node_credential TEXT NULL AFTER address,
  ADD COLUMN communication_protocol SMALLINT NOT NULL DEFAULT 1 AFTER node_credential,
  ADD COLUMN status SMALLINT NOT NULL DEFAULT 0 AFTER communication_protocol,
  ADD COLUMN config JSON NULL AFTER status,
  ADD COLUMN is_enabled TINYINT(1) NOT NULL DEFAULT 1 AFTER config,
  ADD COLUMN remark VARCHAR(255) NOT NULL DEFAULT '' AFTER is_enabled,
  ADD COLUMN last_sync_at DATETIME(3) NULL AFTER last_seen_at,
  ADD COLUMN version VARCHAR(64) NOT NULL DEFAULT '' AFTER last_sync_at;

ALTER TABLE protocol_endpoints
  ADD COLUMN public_port INT NOT NULL DEFAULT 0 AFTER port,
  ADD COLUMN cipher SMALLINT NOT NULL DEFAULT 0 AFTER public_port,
  ADD COLUMN parent_protocol_id BIGINT UNSIGNED NULL AFTER cipher,
  ADD COLUMN optional_config JSON NULL AFTER client_config,
  ADD COLUMN tags JSON NULL AFTER optional_config,
  ADD INDEX idx_protocol_parent (parent_protocol_id),
  ADD CONSTRAINT fk_protocol_parent FOREIGN KEY (parent_protocol_id) REFERENCES protocol_endpoints(id) ON DELETE SET NULL;
UPDATE protocol_endpoints SET public_port = port WHERE public_port = 0;

CREATE TABLE access_groups (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(80) NOT NULL,
  code VARCHAR(80) NOT NULL,
  description VARCHAR(255) NOT NULL DEFAULT '',
  is_enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_access_groups_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE access_group_protocols (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  access_group_id BIGINT UNSIGNED NOT NULL,
  protocol_endpoint_id BIGINT UNSIGNED NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY ux_access_group_protocol (access_group_id, protocol_endpoint_id),
  INDEX idx_access_group_protocol_endpoint (protocol_endpoint_id),
  CONSTRAINT fk_access_group_protocol_group FOREIGN KEY (access_group_id) REFERENCES access_groups(id) ON DELETE CASCADE,
  CONSTRAINT fk_access_group_protocol_endpoint FOREIGN KEY (protocol_endpoint_id) REFERENCES protocol_endpoints(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO access_groups (name, code, description, is_enabled)
SELECT CONCAT(name, ' access'), CONCAT('plan:', id), CONCAT('Access group migrated for plan ', id), is_active FROM plans;

ALTER TABLE plans
  ADD COLUMN access_group_id BIGINT UNSIGNED NULL AFTER description,
  ADD COLUMN traffic_bytes BIGINT NOT NULL DEFAULT 0 AFTER access_group_id,
  ADD COLUMN speed_limit_mbps INT NOT NULL DEFAULT 0 AFTER traffic_bytes,
  ADD COLUMN max_active_subscriptions INT NOT NULL DEFAULT 0 AFTER speed_limit_mbps,
  ADD COLUMN is_renewable TINYINT(1) NOT NULL DEFAULT 1 AFTER max_active_subscriptions,
  ADD COLUMN device_limit INT NOT NULL DEFAULT 1 AFTER is_renewable,
  ADD COLUMN family_limit INT NOT NULL DEFAULT 0 AFTER device_limit,
  ADD COLUMN reset_policy SMALLINT NOT NULL DEFAULT 0 AFTER family_limit,
  ADD COLUMN traffic_calc_mode SMALLINT NOT NULL DEFAULT 0 AFTER reset_policy,
  ADD COLUMN traffic_multiplier_milli BIGINT NOT NULL DEFAULT 1000 AFTER traffic_calc_mode,
  ADD INDEX idx_plans_access_group (access_group_id),
  ADD CONSTRAINT fk_plans_access_group FOREIGN KEY (access_group_id) REFERENCES access_groups(id) ON DELETE RESTRICT;

UPDATE plans p JOIN access_groups g ON g.code = CONCAT('plan:', p.id) SET p.access_group_id = g.id;
UPDATE plans p
JOIN (
  SELECT s.* FROM plan_skus s
  JOIN (SELECT plan_id, MIN(id) AS id FROM plan_skus GROUP BY plan_id) first_sku ON first_sku.id = s.id
) sku ON sku.plan_id = p.id
SET p.traffic_bytes = sku.traffic_bytes,
    p.speed_limit_mbps = sku.speed_limit_mbps,
    p.device_limit = sku.device_limit;

INSERT INTO access_group_protocols (access_group_id, protocol_endpoint_id, sort_order)
SELECT p.access_group_id, binding.protocol_endpoint_id, endpoint.sort_order
FROM plan_protocol_endpoints binding
JOIN plans p ON p.id = binding.plan_id
JOIN protocol_endpoints endpoint ON endpoint.id = binding.protocol_endpoint_id;

ALTER TABLE plans MODIFY COLUMN access_group_id BIGINT UNSIGNED NOT NULL;

ALTER TABLE plan_skus
  ADD COLUMN sku_type VARCHAR(20) NOT NULL DEFAULT 'new' AFTER name;

ALTER TABLE subscriptions
  ADD COLUMN access_group_id BIGINT UNSIGNED NULL AFTER plan_sku_id,
  ADD COLUMN subscription_type SMALLINT NOT NULL DEFAULT 1 AFTER access_group_id,
  ADD COLUMN speed_limit_mbps INT NOT NULL DEFAULT 0 AFTER flow_used,
  ADD COLUMN device_limit INT NOT NULL DEFAULT 1 AFTER speed_limit_mbps,
  ADD COLUMN family_limit INT NOT NULL DEFAULT 0 AFTER device_limit,
  ADD COLUMN renewal_price_minor BIGINT NOT NULL DEFAULT 0 AFTER family_limit,
  ADD COLUMN reset_policy SMALLINT NOT NULL DEFAULT 0 AFTER renewal_price_minor,
  ADD COLUMN next_reset_at DATETIME(3) NULL AFTER reset_policy,
  ADD COLUMN traffic_calc_mode SMALLINT NOT NULL DEFAULT 0 AFTER next_reset_at,
  ADD COLUMN traffic_multiplier_milli BIGINT NOT NULL DEFAULT 1000 AFTER traffic_calc_mode,
  ADD COLUMN config JSON NULL AFTER traffic_multiplier_milli,
  ADD INDEX idx_subscriptions_access_group (access_group_id),
  ADD INDEX idx_subscriptions_reset (next_reset_at),
  ADD CONSTRAINT fk_subscriptions_access_group FOREIGN KEY (access_group_id) REFERENCES access_groups(id) ON DELETE RESTRICT;

UPDATE subscriptions subscription
JOIN plans plan ON plan.id = subscription.plan_id
JOIN plan_skus sku ON sku.id = subscription.plan_sku_id
SET subscription.access_group_id = plan.access_group_id,
    subscription.speed_limit_mbps = sku.speed_limit_mbps,
    subscription.device_limit = sku.device_limit,
    subscription.family_limit = plan.family_limit,
    subscription.renewal_price_minor = IF(plan.is_renewable, sku.price_cents, 0),
    subscription.reset_policy = plan.reset_policy,
    subscription.traffic_calc_mode = plan.traffic_calc_mode,
    subscription.traffic_multiplier_milli = plan.traffic_multiplier_milli;
ALTER TABLE subscriptions MODIFY COLUMN access_group_id BIGINT UNSIGNED NOT NULL;

ALTER TABLE subscription_tokens
  ADD COLUMN subscription_id BIGINT UNSIGNED NULL AFTER user_id,
  ADD INDEX idx_subscription_tokens_subscription (subscription_id),
  ADD CONSTRAINT fk_subscription_tokens_subscription FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE;

ALTER TABLE traffic_records
  ADD COLUMN upload_bytes BIGINT NOT NULL DEFAULT 0 AFTER raw_bytes,
  ADD COLUMN download_bytes BIGINT NOT NULL DEFAULT 0 AFTER upload_bytes,
  ADD COLUMN traffic_calc_mode SMALLINT NOT NULL DEFAULT 0 AFTER download_bytes,
  ADD COLUMN protocol_multiplier_milli BIGINT NOT NULL DEFAULT 1000 AFTER traffic_calc_mode,
  ADD COLUMN subscription_multiplier_milli BIGINT NOT NULL DEFAULT 1000 AFTER protocol_multiplier_milli,
  ADD COLUMN created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) AFTER meta,
  ADD COLUMN updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) AFTER created_at;
UPDATE traffic_records
SET download_bytes = raw_bytes,
    protocol_multiplier_milli = multiplier_milli,
    subscription_multiplier_milli = 1000;

ALTER TABLE orders
  ADD COLUMN order_type VARCHAR(20) NOT NULL DEFAULT 'new' AFTER trade_no,
  ADD COLUMN target_subscription_id BIGINT UNSIGNED NULL AFTER subscription_id,
  ADD COLUMN payable_amount BIGINT NOT NULL DEFAULT 0 AFTER amount_cents,
  ADD COLUMN paid_amount BIGINT NOT NULL DEFAULT 0 AFTER payable_amount,
  ADD COLUMN refund_amount BIGINT NOT NULL DEFAULT 0 AFTER paid_amount,
  ADD COLUMN discount_amount BIGINT NOT NULL DEFAULT 0 AFTER refund_amount,
  ADD COLUMN provider_trade_no VARCHAR(128) NULL AFTER channel,
  ADD COLUMN paid_at DATETIME(3) NULL AFTER raw_callback,
  ADD COLUMN canceled_at DATETIME(3) NULL AFTER paid_at,
  ADD COLUMN fulfilled_at DATETIME(3) NULL AFTER canceled_at,
  ADD COLUMN refunded_at DATETIME(3) NULL AFTER fulfilled_at,
  ADD COLUMN failure_reason VARCHAR(255) NOT NULL DEFAULT '' AFTER refunded_at,
  ADD INDEX idx_orders_target_subscription (target_subscription_id),
  ADD UNIQUE KEY uk_orders_provider_trade_no (provider_trade_no),
  ADD CONSTRAINT fk_orders_target_subscription FOREIGN KEY (target_subscription_id) REFERENCES subscriptions(id) ON DELETE SET NULL;
UPDATE orders SET payable_amount = amount_cents, paid_amount = IF(status = 'paid', amount_cents, 0);

CREATE TABLE payment_events (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  order_id BIGINT UNSIGNED NOT NULL,
  provider VARCHAR(32) NOT NULL,
  provider_event_id VARCHAR(128) NOT NULL,
  event_type VARCHAR(32) NOT NULL,
  amount_minor BIGINT NOT NULL DEFAULT 0,
  signature_valid TINYINT(1) NOT NULL DEFAULT 0,
  payload JSON NULL,
  processed_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY ux_payment_provider_event (provider, provider_event_id),
  INDEX idx_payment_events_order (order_id),
  CONSTRAINT fk_payment_events_order FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE subscription_members (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  subscription_id BIGINT UNSIGNED NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  member_role VARCHAR(16) NOT NULL DEFAULT 'member',
  status VARCHAR(20) NOT NULL DEFAULT 'active',
  joined_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  removed_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY ux_subscription_member (subscription_id, user_id),
  INDEX idx_subscription_members_user (user_id),
  CONSTRAINT fk_subscription_members_subscription FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE,
  CONSTRAINT fk_subscription_members_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE user_api_tokens (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  token_hash VARCHAR(64) NOT NULL,
  token_prefix VARCHAR(12) NOT NULL,
  scopes JSON NULL,
  last_used_at DATETIME(3) NULL,
  revoked_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_user_api_tokens_hash (token_hash),
  INDEX idx_user_api_tokens_user (user_id),
  CONSTRAINT fk_user_api_tokens_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE tasks (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  type VARCHAR(32) NOT NULL,
  scope JSON NULL,
  content JSON NULL,
  status SMALLINT NOT NULL DEFAULT 0,
  errors TEXT NULL,
  total BIGINT NOT NULL DEFAULT 0,
  current BIGINT NOT NULL DEFAULT 0,
  idempotency_key VARCHAR(128) NULL,
  priority INT NOT NULL DEFAULT 0,
  scheduled_at DATETIME(3) NULL,
  started_at DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  attempts INT NOT NULL DEFAULT 0,
  max_attempts INT NOT NULL DEFAULT 3,
  locked_by VARCHAR(128) NULL,
  locked_until DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_tasks_idempotency (idempotency_key),
  INDEX idx_tasks_dispatch (status, scheduled_at, priority),
  INDEX idx_tasks_lock (locked_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE task_items (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  task_id BIGINT UNSIGNED NOT NULL,
  target_type VARCHAR(32) NOT NULL,
  target_id VARCHAR(128) NOT NULL,
  payload JSON NULL,
  status SMALLINT NOT NULL DEFAULT 0,
  attempts INT NOT NULL DEFAULT 0,
  error TEXT NULL,
  started_at DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY ux_task_target (task_id, target_type, target_id),
  INDEX idx_task_items_status (task_id, status),
  CONSTRAINT fk_task_items_task FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE system_configs (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  config_key VARCHAR(80) NOT NULL,
  name VARCHAR(120) NOT NULL,
  value TEXT NULL,
  value_type VARCHAR(16) NOT NULL,
  description VARCHAR(255) NOT NULL DEFAULT '',
  is_public TINYINT(1) NOT NULL DEFAULT 0,
  is_secret TINYINT(1) NOT NULL DEFAULT 0,
  revision BIGINT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_system_configs_key (config_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO system_configs (config_key, name, value, value_type, description, is_public) VALUES
  ('register_switch', '注册开关', 'true', 'bool', '是否允许注册', 1),
  ('site_name', '站点名称', 'zboard', 'string', '用于展示站点名称的位置', 1),
  ('site_desc', '站点描述', '', 'string', '用于展示需要站点描述的地方', 1),
  ('site_logo', '站点 Logo', '', 'string', '网站的 Logo 地址', 1),
  ('site_url', '站点网址', '', 'string', '当前站点的网址', 1),
  ('subscribe_url', '订阅网址', '', 'string', '独立订阅地址；为空时由站点网址生成', 1);

CREATE TABLE protocol_deployments (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  protocol_endpoint_id BIGINT UNSIGNED NOT NULL,
  node_id BIGINT UNSIGNED NOT NULL,
  config_revision BIGINT UNSIGNED NOT NULL,
  status VARCHAR(20) NOT NULL,
  requested_by BIGINT UNSIGNED NULL,
  output TEXT NULL,
  error TEXT NULL,
  started_at DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_protocol_deployments_endpoint (protocol_endpoint_id, config_revision),
  INDEX idx_protocol_deployments_node (node_id, status),
  CONSTRAINT fk_protocol_deployments_endpoint FOREIGN KEY (protocol_endpoint_id) REFERENCES protocol_endpoints(id) ON DELETE CASCADE,
  CONSTRAINT fk_protocol_deployments_node FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE,
  CONSTRAINT fk_protocol_deployments_user FOREIGN KEY (requested_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE quota_events (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  subscription_id BIGINT UNSIGNED NOT NULL,
  event_type VARCHAR(24) NOT NULL,
  delta_bytes BIGINT NOT NULL,
  balance_before BIGINT NOT NULL,
  balance_after BIGINT NOT NULL,
  reference_type VARCHAR(32) NOT NULL,
  reference_id VARCHAR(128) NOT NULL,
  detail JSON NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY ux_quota_event_reference (subscription_id, event_type, reference_type, reference_id),
  INDEX idx_quota_events_subscription (subscription_id, created_at),
  CONSTRAINT fk_quota_events_subscription FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
