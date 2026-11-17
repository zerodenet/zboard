ALTER TABLE node_config_publishes ADD COLUMN phase TEXT NOT NULL DEFAULT '';
ALTER TABLE node_config_publishes ADD COLUMN applied_config_sha256 TEXT NOT NULL DEFAULT '';
