ALTER TABLE protocol_credentials ADD COLUMN principal_digest varchar(64) NOT NULL DEFAULT '';
ALTER TABLE protocol_credentials ADD COLUMN secret_principal_digest varchar(64) NOT NULL DEFAULT '';
ALTER TABLE protocol_credentials ADD COLUMN principal_lookup_version tinyint unsigned NOT NULL DEFAULT 0;
CREATE INDEX idx_credential_principal_digest ON protocol_credentials(node_id,principal_digest);
CREATE INDEX idx_credential_secret_digest ON protocol_credentials(node_id,secret_principal_digest);
CREATE INDEX idx_credential_lookup_backfill ON protocol_credentials(node_id,principal_lookup_version,id);
