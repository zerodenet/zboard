ALTER TABLE principal_flow_observations ADD COLUMN core_instance_sha256 TEXT NOT NULL DEFAULT '';
-- The owned runner backfills/verifies before creating this complete index.
CREATE UNIQUE INDEX uk_principal_flow_observation_runtime_revision ON principal_flow_observations(node_id,core_instance_sha256,session_registry_revision,principal_key);
-- SQLite already retains full raw TEXT and its original inline unique key.
SELECT 1;
