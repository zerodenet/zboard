ALTER TABLE protocol_deployments ADD COLUMN credential_targets LONGTEXT NOT NULL DEFAULT ('');
ALTER TABLE protocol_credentials ADD COLUMN delivery_required boolean NOT NULL DEFAULT false;
ALTER TABLE network_entries ADD COLUMN delivery_required boolean NOT NULL DEFAULT false;
CREATE TABLE credential_delivery_confirmations (
 credential_id bigint unsigned NOT NULL PRIMARY KEY,
 node_id bigint unsigned NOT NULL,
 protocol_endpoint_id bigint unsigned NOT NULL,
 deployment_id bigint unsigned NOT NULL,
 material_sha256 varchar(64) NOT NULL,
 endpoint_sha256 varchar(64) NOT NULL,
 config_sha256 varchar(64) NOT NULL,
 policy_revision bigint unsigned NOT NULL,
 confirmed_at datetime(3) NOT NULL,
 INDEX idx_credential_delivery_confirmations_node_id(node_id),
 INDEX idx_credential_delivery_confirmations_deployment_id(deployment_id)
);
CREATE TABLE node_credential_delivery_states (
 node_id bigint unsigned NOT NULL PRIMARY KEY,
 deployment_id bigint unsigned NOT NULL,
 config_sha256 varchar(64) NOT NULL,
 confirmed_at datetime(3) NOT NULL
);

CREATE INDEX idx_deployment_node_status_id ON protocol_deployments(node_id,status,id);

CREATE TABLE network_entry_delivery_confirmations (
 entry_id bigint unsigned NOT NULL PRIMARY KEY,
 node_id bigint unsigned NOT NULL,
 deployment_id bigint unsigned NOT NULL,
 material_sha256 varchar(64) NOT NULL,
 config_sha256 varchar(64) NOT NULL,
 confirmed_at datetime(3) NOT NULL,
 INDEX idx_network_entry_delivery_confirmations_node_id(node_id)
);
