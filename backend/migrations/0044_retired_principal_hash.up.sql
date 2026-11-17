ALTER TABLE protocol_credentials ADD COLUMN retired_principal_hash varchar(64) NOT NULL DEFAULT '';
CREATE INDEX idx_credential_retired_principal ON protocol_credentials(node_id,retired_principal_hash);
