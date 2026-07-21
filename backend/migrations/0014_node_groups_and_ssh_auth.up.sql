ALTER TABLE access_group_protocols
  DROP FOREIGN KEY fk_access_group_protocol_group,
  DROP FOREIGN KEY fk_access_group_protocol_endpoint;

ALTER TABLE plans DROP FOREIGN KEY fk_plans_access_group;
ALTER TABLE subscriptions DROP FOREIGN KEY fk_subscriptions_access_group;

RENAME TABLE access_groups TO node_groups,
             access_group_protocols TO node_group_endpoints;

ALTER TABLE node_group_endpoints
  CHANGE COLUMN access_group_id node_group_id BIGINT UNSIGNED NOT NULL,
  DROP INDEX ux_access_group_protocol,
  DROP INDEX idx_access_group_protocol_endpoint,
  ADD UNIQUE KEY ux_node_group_endpoint (node_group_id, protocol_endpoint_id),
  ADD INDEX idx_node_group_endpoint_endpoint (protocol_endpoint_id),
  ADD CONSTRAINT fk_node_group_endpoint_group FOREIGN KEY (node_group_id) REFERENCES node_groups(id) ON DELETE CASCADE,
  ADD CONSTRAINT fk_node_group_endpoint_endpoint FOREIGN KEY (protocol_endpoint_id) REFERENCES protocol_endpoints(id) ON DELETE CASCADE;

ALTER TABLE plans
  CHANGE COLUMN access_group_id node_group_id BIGINT UNSIGNED NOT NULL,
  DROP INDEX idx_plans_access_group,
  ADD INDEX idx_plans_node_group (node_group_id),
  ADD CONSTRAINT fk_plans_node_group FOREIGN KEY (node_group_id) REFERENCES node_groups(id) ON DELETE RESTRICT,
  DROP COLUMN traffic_multiplier_milli;

ALTER TABLE subscriptions
  CHANGE COLUMN access_group_id node_group_id BIGINT UNSIGNED NOT NULL,
  DROP INDEX idx_subscriptions_access_group,
  ADD INDEX idx_subscriptions_node_group (node_group_id),
  ADD CONSTRAINT fk_subscriptions_node_group FOREIGN KEY (node_group_id) REFERENCES node_groups(id) ON DELETE RESTRICT,
  DROP COLUMN traffic_multiplier_milli;

ALTER TABLE traffic_records
  DROP COLUMN subscription_multiplier_milli,
  DROP COLUMN multiplier_milli;

CREATE TEMPORARY TABLE migration_0014_plan_group_rewrites (
  plan_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  old_node_group_id BIGINT UNSIGNED NOT NULL,
  new_node_group_id BIGINT UNSIGNED NULL
) ENGINE=InnoDB;

-- plan_protocol_endpoints remained the delivery source after access groups were
-- introduced. When the two sets drifted, give that plan a private node group so
-- removing the direct binding cannot broaden or shrink its delivered endpoints.
INSERT INTO migration_0014_plan_group_rewrites (plan_id, old_node_group_id)
SELECT plan.id, plan.node_group_id
FROM plans plan
WHERE EXISTS (
  SELECT 1
  FROM plan_protocol_endpoints direct_binding
  LEFT JOIN node_group_endpoints group_binding
    ON group_binding.node_group_id = plan.node_group_id
   AND group_binding.protocol_endpoint_id = direct_binding.protocol_endpoint_id
  WHERE direct_binding.plan_id = plan.id
    AND group_binding.id IS NULL
)
OR EXISTS (
  SELECT 1
  FROM node_group_endpoints group_binding
  LEFT JOIN plan_protocol_endpoints direct_binding
    ON direct_binding.plan_id = plan.id
   AND direct_binding.protocol_endpoint_id = group_binding.protocol_endpoint_id
  WHERE group_binding.node_group_id = plan.node_group_id
    AND direct_binding.id IS NULL
);

INSERT INTO node_groups (name, code, description, is_enabled)
SELECT LEFT(CONCAT(source_group.name, ' / plan ', plan.id), 80),
       CONCAT('__0014_plan_', plan.id, '_', rewrite.old_node_group_id),
       LEFT(CONCAT('Migration-preserved endpoint boundary for plan ', plan.id, '. ', source_group.description), 255),
       source_group.is_enabled
FROM migration_0014_plan_group_rewrites rewrite
JOIN plans plan ON plan.id = rewrite.plan_id
JOIN node_groups source_group ON source_group.id = rewrite.old_node_group_id;

UPDATE migration_0014_plan_group_rewrites rewrite
JOIN node_groups new_group
  ON new_group.code = CONCAT('__0014_plan_', rewrite.plan_id, '_', rewrite.old_node_group_id)
SET rewrite.new_node_group_id = new_group.id;

INSERT INTO node_group_endpoints (node_group_id, protocol_endpoint_id, sort_order, created_at)
SELECT rewrite.new_node_group_id, direct_binding.protocol_endpoint_id, endpoint.sort_order, direct_binding.created_at
FROM migration_0014_plan_group_rewrites rewrite
JOIN plan_protocol_endpoints direct_binding ON direct_binding.plan_id = rewrite.plan_id
JOIN protocol_endpoints endpoint ON endpoint.id = direct_binding.protocol_endpoint_id;

UPDATE subscriptions subscription
JOIN migration_0014_plan_group_rewrites rewrite
  ON rewrite.plan_id = subscription.plan_id
 AND rewrite.old_node_group_id = subscription.node_group_id
SET subscription.node_group_id = rewrite.new_node_group_id;

UPDATE plans plan
JOIN migration_0014_plan_group_rewrites rewrite ON rewrite.plan_id = plan.id
SET plan.node_group_id = rewrite.new_node_group_id;

DROP TEMPORARY TABLE migration_0014_plan_group_rewrites;

DROP TABLE plan_protocol_endpoints;

ALTER TABLE nodes
  ADD COLUMN lifecycle_status VARCHAR(20) NOT NULL DEFAULT 'active' AFTER status,
  ADD COLUMN ssh_auth_method VARCHAR(20) NOT NULL DEFAULT 'password' AFTER ssh_user,
  ADD COLUMN ssh_private_key_passphrase TEXT NULL AFTER ssh_pwd,
  ADD COLUMN ssh_host_key_policy VARCHAR(24) NOT NULL DEFAULT 'trust_on_first_use' AFTER ssh_host_key_fingerprint;

UPDATE nodes
SET ssh_host_key_policy = CASE
  WHEN ssh_host_key_fingerprint IS NULL OR ssh_host_key_fingerprint = '' THEN 'trust_on_first_use'
  ELSE 'strict'
END;
