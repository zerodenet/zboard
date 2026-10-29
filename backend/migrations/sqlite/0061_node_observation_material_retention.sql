ALTER TABLE node_observation_intents ADD COLUMN material_retired_at datetime NULL;
ALTER TABLE node_observation_intents ADD COLUMN material_review_at datetime NOT NULL DEFAULT '1970-01-01 00:00:00.000';
CREATE INDEX idx_observation_material_review ON node_observation_intents(status,material_retired_at,material_review_at,id);
CREATE INDEX idx_observation_latest_reference ON node_observation_states(latest_reference);
CREATE INDEX idx_observation_stats_reference ON node_observation_states(stats_reference);
CREATE INDEX idx_observation_floor_reference ON node_observation_states(floor_reference);
