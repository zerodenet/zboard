-- Auxiliary retained-material authority. No financial/state FK or cascade.
CREATE TABLE IF NOT EXISTS node_observation_material_budget (
 id bigint unsigned NOT NULL PRIMARY KEY, retained_bytes bigint unsigned NOT NULL, revision bigint unsigned NOT NULL,
 CONSTRAINT chk_observation_budget_singleton CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS node_observation_retirements (
 intent_id varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
 header_sha256 varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 state varchar(24) NOT NULL, cause varchar(64) NOT NULL, source_summary text NOT NULL,
 next_part bigint unsigned NOT NULL, verified_bytes bigint unsigned NOT NULL, hash_state blob NOT NULL,
 delete_part bigint unsigned NOT NULL, remaining_bytes bigint unsigned NOT NULL, content_verified tinyint(1) NOT NULL,
 review_at datetime(3) NOT NULL, created_at datetime(3) NOT NULL, updated_at datetime(3) NOT NULL,
 KEY idx_observation_retirement_ready (state,review_at,intent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- Owned reconciliation seeds actual retained bytes once, after DDL is complete.
CREATE TABLE IF NOT EXISTS node_observation_retirement_parts (
 intent_id varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 ordinal bigint unsigned NOT NULL, sha256 varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 PRIMARY KEY (intent_id,ordinal)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
