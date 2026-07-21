ALTER TABLE users DROP COLUMN username;
ALTER TABLE audit_logs MODIFY COLUMN actor VARCHAR(128) NOT NULL;

ALTER TABLE plans
  ADD COLUMN slug VARCHAR(80) NULL AFTER name,
  ADD COLUMN summary VARCHAR(255) NOT NULL DEFAULT '' AFTER slug,
  ADD COLUMN description TEXT NULL AFTER summary,
  ADD COLUMN sort_order INT NOT NULL DEFAULT 0 AFTER is_active;
UPDATE plans SET slug = CONCAT('legacy-', id) WHERE slug IS NULL OR slug = '';
ALTER TABLE plans MODIFY COLUMN slug VARCHAR(80) NOT NULL, ADD UNIQUE KEY uk_plans_slug (slug);

CREATE TABLE plan_skus (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  plan_id BIGINT UNSIGNED NOT NULL,
  code VARCHAR(80) NOT NULL,
  name VARCHAR(80) NOT NULL,
  billing_unit VARCHAR(16) NOT NULL,
  billing_value INT NOT NULL,
  price_cents BIGINT NOT NULL,
  currency VARCHAR(8) NOT NULL,
  traffic_bytes BIGINT NOT NULL,
  device_limit INT NOT NULL,
  speed_limit_mbps INT NOT NULL DEFAULT 0,
  is_active TINYINT(1) NOT NULL DEFAULT 1,
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_plan_skus_code (code),
  INDEX idx_plan_skus_plan (plan_id),
  CONSTRAINT fk_plan_skus_plan FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO plan_skus (plan_id, code, name, billing_unit, billing_value, price_cents, currency, traffic_bytes, device_limit, is_active)
SELECT id, CONCAT('legacy-', id), CONCAT(name, ' legacy'), 'day', duration_day, price_cents, 'USD', traffic_gb * 1073741824, max_device, is_active
FROM plans;

ALTER TABLE subscriptions ADD COLUMN plan_sku_id BIGINT UNSIGNED NULL AFTER plan_id;
UPDATE subscriptions s JOIN plan_skus sku ON sku.plan_id = s.plan_id SET s.plan_sku_id = sku.id;
ALTER TABLE subscriptions
  MODIFY COLUMN plan_sku_id BIGINT UNSIGNED NOT NULL,
  ADD INDEX idx_subscriptions_plan_sku (plan_sku_id),
  ADD CONSTRAINT fk_subscriptions_plan_sku FOREIGN KEY (plan_sku_id) REFERENCES plan_skus(id) ON DELETE RESTRICT;

ALTER TABLE orders
  ADD COLUMN plan_sku_id BIGINT UNSIGNED NULL AFTER plan_id,
  ADD COLUMN plan_name VARCHAR(80) NOT NULL DEFAULT '' AFTER status,
  ADD COLUMN sku_name VARCHAR(80) NOT NULL DEFAULT '' AFTER plan_name,
  ADD COLUMN billing_unit VARCHAR(16) NOT NULL DEFAULT '' AFTER sku_name,
  ADD COLUMN billing_value INT NOT NULL DEFAULT 0 AFTER billing_unit,
  ADD COLUMN traffic_bytes BIGINT NOT NULL DEFAULT 0 AFTER billing_value,
  ADD COLUMN device_limit INT NOT NULL DEFAULT 0 AFTER traffic_bytes,
  ADD COLUMN speed_limit_mbps INT NOT NULL DEFAULT 0 AFTER device_limit;
UPDATE orders o
JOIN plans p ON p.id = o.plan_id
JOIN plan_skus sku ON sku.plan_id = p.id
SET o.plan_sku_id = sku.id, o.plan_name = p.name, o.sku_name = sku.name,
    o.billing_unit = sku.billing_unit, o.billing_value = sku.billing_value,
    o.traffic_bytes = sku.traffic_bytes, o.device_limit = sku.device_limit,
    o.speed_limit_mbps = sku.speed_limit_mbps;
ALTER TABLE orders
  MODIFY COLUMN plan_sku_id BIGINT UNSIGNED NOT NULL,
  ADD INDEX idx_orders_plan_sku (plan_sku_id),
  ADD CONSTRAINT fk_orders_plan_sku FOREIGN KEY (plan_sku_id) REFERENCES plan_skus(id) ON DELETE RESTRICT;

CREATE TABLE protocol_endpoints (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  node_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(80) NOT NULL,
	  runtime_key VARCHAR(36) NOT NULL,
  protocol VARCHAR(32) NOT NULL,
  address VARCHAR(255) NOT NULL,
  port INT NOT NULL,
  multiplier_milli BIGINT NOT NULL DEFAULT 1000,
  server_config TEXT NULL,
  client_config TEXT NULL,
  is_active TINYINT(1) NOT NULL DEFAULT 1,
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_protocol_endpoints_node (node_id),
	  UNIQUE KEY uk_protocol_endpoints_runtime_key (runtime_key),
  CONSTRAINT fk_protocol_endpoints_node FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO protocol_endpoints (node_id, name, runtime_key, protocol, address, port, multiplier_milli, server_config, client_config, is_active)
SELECT id, CONCAT(name, ' ', protocol), UUID(), protocol, address, 443, 1000, protocol_config, client_config, is_online
FROM nodes WHERE protocol IS NOT NULL AND protocol <> '';

CREATE TABLE plan_protocol_endpoints (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  plan_id BIGINT UNSIGNED NOT NULL,
  protocol_endpoint_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY ux_plan_endpoint (plan_id, protocol_endpoint_id),
  INDEX idx_plan_protocol_endpoint_endpoint (protocol_endpoint_id),
  CONSTRAINT fk_plan_protocol_endpoint_plan FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
  CONSTRAINT fk_plan_protocol_endpoint_endpoint FOREIGN KEY (protocol_endpoint_id) REFERENCES protocol_endpoints(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT INTO plan_protocol_endpoints (plan_id, protocol_endpoint_id)
SELECT p.id, e.id FROM plans p CROSS JOIN protocol_endpoints e;

ALTER TABLE traffic_records
  ADD COLUMN protocol_endpoint_id BIGINT UNSIGNED NULL AFTER node_id,
  ADD COLUMN raw_bytes BIGINT NOT NULL DEFAULT 0 AFTER nonce,
  ADD COLUMN multiplier_milli BIGINT NOT NULL DEFAULT 1000 AFTER raw_bytes;
UPDATE traffic_records t
LEFT JOIN protocol_endpoints e ON e.node_id = t.node_id
SET t.protocol_endpoint_id = e.id, t.raw_bytes = t.used_bytes, t.multiplier_milli = 1000;
ALTER TABLE traffic_records
  ADD INDEX idx_traffic_protocol_endpoint (protocol_endpoint_id),
  ADD CONSTRAINT fk_traffic_protocol_endpoint FOREIGN KEY (protocol_endpoint_id) REFERENCES protocol_endpoints(id) ON DELETE SET NULL;

ALTER TABLE plans DROP COLUMN price_cents, DROP COLUMN traffic_gb, DROP COLUMN duration_day, DROP COLUMN max_device;
ALTER TABLE nodes DROP COLUMN protocol, DROP COLUMN protocol_config, DROP COLUMN client_config;
