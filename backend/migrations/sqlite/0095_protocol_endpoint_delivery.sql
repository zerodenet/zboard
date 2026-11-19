ALTER TABLE protocol_endpoints ADD COLUMN delivery_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE protocol_deployments ADD COLUMN protocol_endpoint_targets TEXT NULL;
