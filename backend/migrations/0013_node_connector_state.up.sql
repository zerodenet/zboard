ALTER TABLE nodes
  ADD COLUMN node_credential_prefix VARCHAR(12) NULL AFTER node_credential,
  ADD COLUMN node_credential_revoked_at DATETIME(3) NULL AFTER node_credential_prefix,
  ADD COLUMN ssh_verified_at DATETIME(3) NULL AFTER ssh_host_key_fingerprint,
  ADD COLUMN connector_last_seen_at DATETIME(3) NULL AFTER ssh_verified_at,
  ADD COLUMN uptime_seconds BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER connector_last_seen_at,
  ADD COLUMN active_flows BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER uptime_seconds,
  ADD COLUMN bytes_up BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER active_flows,
  ADD COLUMN bytes_down BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER bytes_up;

CREATE INDEX idx_nodes_connector_last_seen ON nodes (connector_last_seen_at);
