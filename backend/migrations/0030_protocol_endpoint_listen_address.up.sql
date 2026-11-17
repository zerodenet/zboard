ALTER TABLE protocol_endpoints ADD COLUMN listen_address VARCHAR(64) NOT NULL DEFAULT '0.0.0.0';
