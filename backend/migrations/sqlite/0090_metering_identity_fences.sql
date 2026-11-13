CREATE TABLE IF NOT EXISTS metering_identity_fences (node_id INTEGER NOT NULL PRIMARY KEY);
INSERT OR IGNORE INTO metering_identity_fences(node_id) SELECT id FROM nodes;
