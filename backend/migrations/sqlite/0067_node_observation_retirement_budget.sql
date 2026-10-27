CREATE TABLE IF NOT EXISTS node_observation_material_budget (
 id integer NOT NULL PRIMARY KEY CHECK (id = 1), retained_bytes integer NOT NULL, revision integer NOT NULL
);
CREATE TABLE IF NOT EXISTS node_observation_retirements (
 intent_id text NOT NULL PRIMARY KEY, header_sha256 text NOT NULL, state text NOT NULL, cause text NOT NULL, source_summary text NOT NULL,
 next_part integer NOT NULL, verified_bytes integer NOT NULL, hash_state blob NOT NULL,
 delete_part integer NOT NULL, remaining_bytes integer NOT NULL, content_verified numeric NOT NULL,
 review_at datetime NOT NULL, created_at datetime NOT NULL, updated_at datetime NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_observation_retirement_ready ON node_observation_retirements(state,review_at,intent_id);
CREATE TABLE IF NOT EXISTS node_observation_retirement_parts (
 intent_id text NOT NULL, ordinal integer NOT NULL, sha256 text NOT NULL,
 PRIMARY KEY (intent_id,ordinal)
);
