ALTER TABLE protocol_deployments ADD COLUMN credential_targets TEXT NOT NULL DEFAULT '';
ALTER TABLE protocol_credentials ADD COLUMN delivery_required INTEGER NOT NULL DEFAULT false;
ALTER TABLE network_entries ADD COLUMN delivery_required INTEGER NOT NULL DEFAULT false;
CREATE TABLE credential_delivery_confirmations (
 credential_id INTEGER NOT NULL PRIMARY KEY,
 node_id INTEGER NOT NULL,
 protocol_endpoint_id INTEGER NOT NULL,
 deployment_id INTEGER NOT NULL,
 material_sha256 TEXT NOT NULL,
 endpoint_sha256 TEXT NOT NULL,
 config_sha256 TEXT NOT NULL,
 policy_revision INTEGER NOT NULL,
 confirmed_at DATETIME NOT NULL
);
CREATE TABLE node_credential_delivery_states (
 node_id INTEGER NOT NULL PRIMARY KEY,
 deployment_id INTEGER NOT NULL,
 config_sha256 TEXT NOT NULL,
 confirmed_at DATETIME NOT NULL
);

CREATE INDEX idx_deployment_node_status_id ON protocol_deployments(node_id,status,id);

CREATE TABLE network_entry_delivery_confirmations (
 entry_id INTEGER NOT NULL PRIMARY KEY,
 node_id INTEGER NOT NULL,
 deployment_id INTEGER NOT NULL,
 material_sha256 TEXT NOT NULL,
 config_sha256 TEXT NOT NULL,
 confirmed_at DATETIME NOT NULL
);
CREATE INDEX idx_credential_delivery_confirmations_node_id ON credential_delivery_confirmations(node_id);
CREATE INDEX idx_credential_delivery_confirmations_deployment_id ON credential_delivery_confirmations(deployment_id);
CREATE INDEX idx_network_entry_delivery_confirmations_node_id ON network_entry_delivery_confirmations(node_id);
