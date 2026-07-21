ALTER TABLE nodes
  ADD COLUMN protocol VARCHAR(32) NOT NULL DEFAULT '',
  ADD COLUMN protocol_config TEXT NULL,
  ADD COLUMN client_config TEXT NULL;
UPDATE nodes n JOIN protocol_endpoints e ON e.node_id = n.id
SET n.protocol = e.protocol, n.protocol_config = e.server_config, n.client_config = e.client_config;

ALTER TABLE plans
  ADD COLUMN price_cents BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN traffic_gb BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN duration_day INT NOT NULL DEFAULT 30,
  ADD COLUMN max_device INT NOT NULL DEFAULT 1;
UPDATE plans p JOIN plan_skus sku ON sku.plan_id = p.id
SET p.price_cents = sku.price_cents, p.traffic_gb = FLOOR(sku.traffic_bytes / 1073741824),
    p.duration_day = CASE sku.billing_unit WHEN 'day' THEN sku.billing_value WHEN 'month' THEN sku.billing_value * 30 ELSE sku.billing_value * 365 END,
    p.max_device = sku.device_limit;

ALTER TABLE traffic_records DROP FOREIGN KEY fk_traffic_protocol_endpoint, DROP COLUMN protocol_endpoint_id, DROP COLUMN raw_bytes, DROP COLUMN multiplier_milli;
DROP TABLE plan_protocol_endpoints;
DROP TABLE protocol_endpoints;
ALTER TABLE orders DROP FOREIGN KEY fk_orders_plan_sku, DROP COLUMN plan_sku_id, DROP COLUMN plan_name, DROP COLUMN sku_name, DROP COLUMN billing_unit, DROP COLUMN billing_value, DROP COLUMN traffic_bytes, DROP COLUMN device_limit, DROP COLUMN speed_limit_mbps;
ALTER TABLE subscriptions DROP FOREIGN KEY fk_subscriptions_plan_sku, DROP COLUMN plan_sku_id;
DROP TABLE plan_skus;
ALTER TABLE plans DROP INDEX uk_plans_slug, DROP COLUMN slug, DROP COLUMN summary, DROP COLUMN description, DROP COLUMN sort_order;
ALTER TABLE users ADD COLUMN username VARCHAR(64) NULL UNIQUE AFTER id;
