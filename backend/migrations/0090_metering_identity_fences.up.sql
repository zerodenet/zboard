CREATE TABLE metering_identity_fences (
 node_id BIGINT UNSIGNED NOT NULL PRIMARY KEY
) ENGINE=InnoDB;
INSERT INTO metering_identity_fences(node_id) SELECT id FROM nodes;
