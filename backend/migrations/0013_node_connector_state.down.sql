DROP INDEX idx_nodes_connector_last_seen ON nodes;

ALTER TABLE nodes
  DROP COLUMN bytes_down,
  DROP COLUMN bytes_up,
  DROP COLUMN active_flows,
  DROP COLUMN uptime_seconds,
  DROP COLUMN connector_last_seen_at,
  DROP COLUMN ssh_verified_at,
  DROP COLUMN node_credential_revoked_at,
  DROP COLUMN node_credential_prefix;
