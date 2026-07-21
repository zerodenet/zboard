ALTER TABLE nodes
  DROP COLUMN ssh_host_key_policy,
  DROP COLUMN ssh_private_key_passphrase,
  DROP COLUMN ssh_auth_method,
  DROP COLUMN lifecycle_status;

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
SELECT plan.id, binding.protocol_endpoint_id
FROM plans plan
JOIN node_group_endpoints binding ON binding.node_group_id = plan.node_group_id;

-- Restore the original access-group reference for plans that needed a private
-- migration group. Direct endpoint bindings above retain their current boundary.
UPDATE subscriptions subscription
JOIN plans plan
  ON plan.id = subscription.plan_id
 AND subscription.node_group_id = plan.node_group_id
JOIN node_groups cloned_group
  ON cloned_group.id = plan.node_group_id
 AND LEFT(cloned_group.code, 12) = '__0014_plan_'
SET subscription.node_group_id = CAST(SUBSTRING_INDEX(cloned_group.code, '_', -1) AS UNSIGNED);

UPDATE plans plan
JOIN node_groups cloned_group
  ON cloned_group.id = plan.node_group_id
 AND LEFT(cloned_group.code, 12) = '__0014_plan_'
SET plan.node_group_id = CAST(SUBSTRING_INDEX(cloned_group.code, '_', -1) AS UNSIGNED);

DELETE cloned_group
FROM node_groups cloned_group
LEFT JOIN plans plan ON plan.node_group_id = cloned_group.id
LEFT JOIN subscriptions subscription ON subscription.node_group_id = cloned_group.id
WHERE LEFT(cloned_group.code, 12) = '__0014_plan_'
  AND plan.id IS NULL
  AND subscription.id IS NULL;

ALTER TABLE plans
  DROP FOREIGN KEY fk_plans_node_group,
  ADD COLUMN traffic_multiplier_milli BIGINT NOT NULL DEFAULT 1000 AFTER traffic_calc_mode;

ALTER TABLE subscriptions
  DROP FOREIGN KEY fk_subscriptions_node_group,
  ADD COLUMN traffic_multiplier_milli BIGINT NOT NULL DEFAULT 1000 AFTER traffic_calc_mode;

ALTER TABLE traffic_records
  ADD COLUMN multiplier_milli BIGINT NOT NULL DEFAULT 1000 AFTER raw_bytes,
  ADD COLUMN subscription_multiplier_milli BIGINT NOT NULL DEFAULT 1000 AFTER protocol_multiplier_milli;

UPDATE traffic_records SET multiplier_milli = protocol_multiplier_milli;

ALTER TABLE node_group_endpoints
  DROP FOREIGN KEY fk_node_group_endpoint_group,
  DROP FOREIGN KEY fk_node_group_endpoint_endpoint;

RENAME TABLE node_groups TO access_groups,
             node_group_endpoints TO access_group_protocols;

ALTER TABLE access_group_protocols
  CHANGE COLUMN node_group_id access_group_id BIGINT UNSIGNED NOT NULL,
  DROP INDEX ux_node_group_endpoint,
  DROP INDEX idx_node_group_endpoint_endpoint,
  ADD UNIQUE KEY ux_access_group_protocol (access_group_id, protocol_endpoint_id),
  ADD INDEX idx_access_group_protocol_endpoint (protocol_endpoint_id),
  ADD CONSTRAINT fk_access_group_protocol_group FOREIGN KEY (access_group_id) REFERENCES access_groups(id) ON DELETE CASCADE,
  ADD CONSTRAINT fk_access_group_protocol_endpoint FOREIGN KEY (protocol_endpoint_id) REFERENCES protocol_endpoints(id) ON DELETE CASCADE;

ALTER TABLE plans
  CHANGE COLUMN node_group_id access_group_id BIGINT UNSIGNED NOT NULL,
  DROP INDEX idx_plans_node_group,
  ADD INDEX idx_plans_access_group (access_group_id),
  ADD CONSTRAINT fk_plans_access_group FOREIGN KEY (access_group_id) REFERENCES access_groups(id) ON DELETE RESTRICT;

ALTER TABLE subscriptions
  CHANGE COLUMN node_group_id access_group_id BIGINT UNSIGNED NOT NULL,
  DROP INDEX idx_subscriptions_node_group,
  ADD INDEX idx_subscriptions_access_group (access_group_id),
  ADD CONSTRAINT fk_subscriptions_access_group FOREIGN KEY (access_group_id) REFERENCES access_groups(id) ON DELETE RESTRICT;
