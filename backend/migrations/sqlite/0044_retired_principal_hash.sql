ALTER TABLE protocol_credentials ADD COLUMN retired_principal_hash text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_credential_retired_principal ON protocol_credentials(node_id,retired_principal_hash);
