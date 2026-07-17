ALTER TABLE traffic_records
  DROP INDEX ux_traffic_node_nonce,
  DROP INDEX ux_traffic_node_report,
  DROP COLUMN nonce,
  DROP COLUMN report_id;

ALTER TABLE nodes
  DROP COLUMN traffic_secret_revoked_at,
  DROP COLUMN traffic_secret_prefix,
  DROP COLUMN traffic_secret;
