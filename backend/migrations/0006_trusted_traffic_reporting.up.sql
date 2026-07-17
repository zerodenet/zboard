ALTER TABLE nodes
  ADD COLUMN IF NOT EXISTS traffic_secret TEXT NULL AFTER ssh_host_key_fingerprint,
  ADD COLUMN IF NOT EXISTS traffic_secret_prefix VARCHAR(12) NULL AFTER traffic_secret,
  ADD COLUMN IF NOT EXISTS traffic_secret_revoked_at DATETIME(3) NULL AFTER traffic_secret_prefix;

ALTER TABLE traffic_records
  ADD COLUMN IF NOT EXISTS report_id VARCHAR(64) NULL AFTER node_id,
  ADD COLUMN IF NOT EXISTS nonce VARCHAR(64) NULL AFTER report_id,
  ADD UNIQUE INDEX ux_traffic_node_report (node_id, report_id),
  ADD UNIQUE INDEX ux_traffic_node_nonce (node_id, nonce);
