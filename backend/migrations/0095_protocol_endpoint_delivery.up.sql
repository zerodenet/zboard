ALTER TABLE protocol_endpoints ADD COLUMN delivery_revision BIGINT UNSIGNED NOT NULL DEFAULT 0;
ALTER TABLE protocol_deployments ADD COLUMN protocol_endpoint_targets LONGTEXT NULL;
