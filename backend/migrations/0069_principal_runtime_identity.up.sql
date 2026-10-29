-- The owned Go runner verifies any existing nonempty digest, backfills in
-- bounded pages, and resumes interrupted DDL without erasing old uniqueness.
ALTER TABLE principal_flow_observations ADD COLUMN core_instance_sha256 char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '';
UPDATE principal_flow_observations SET core_instance_sha256 = SHA2(core_instance_id,256) WHERE core_instance_sha256 = '';
CREATE UNIQUE INDEX uk_principal_flow_observation_runtime_revision ON principal_flow_observations(node_id,core_instance_sha256,session_registry_revision,principal_key);
ALTER TABLE principal_flow_node_generations DROP INDEX idx_principal_flow_generation_instance, MODIFY COLUMN core_instance_id MEDIUMTEXT NOT NULL, ADD INDEX idx_principal_flow_generation_instance(core_instance_id(128));
ALTER TABLE principal_flow_currents DROP INDEX idx_principal_flow_current_instance, MODIFY COLUMN core_instance_id MEDIUMTEXT NOT NULL, ADD INDEX idx_principal_flow_current_instance(node_id,core_instance_id(128));
ALTER TABLE principal_flow_scope_observations MODIFY COLUMN core_instance_id MEDIUMTEXT NOT NULL DEFAULT ('');
-- The replacement unique constraint is already complete before the original
-- constraint is removed. One atomic ALTER retains it across interruptions.
ALTER TABLE principal_flow_observations DROP INDEX uk_principal_flow_observation_revision, MODIFY COLUMN core_instance_id MEDIUMTEXT NOT NULL;
