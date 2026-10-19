ALTER TABLE node_config_publishes ADD COLUMN phase varchar(20) NOT NULL DEFAULT '';
ALTER TABLE node_config_publishes ADD COLUMN applied_config_sha256 varchar(64) NOT NULL DEFAULT '';
