-- Preserve the already accepted opaque runtime identity; these two columns
-- have no core-instance indexes, so there is no prefix collision or key growth.
-- The authenticated Zero request remains bounded by its existing body limit.
ALTER TABLE fair_use_node_coverage MODIFY COLUMN core_instance_id MEDIUMTEXT NOT NULL DEFAULT ('');
ALTER TABLE subscription_flow_start_events MODIFY COLUMN core_instance_id MEDIUMTEXT NOT NULL DEFAULT ('');
