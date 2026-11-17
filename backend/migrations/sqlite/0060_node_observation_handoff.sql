CREATE TABLE IF NOT EXISTS node_observation_intents (
 id TEXT NOT NULL PRIMARY KEY, node_id INTEGER NOT NULL, asset_tag TEXT NOT NULL,
 status TEXT NOT NULL, available_at DATETIME NOT NULL, token TEXT NOT NULL DEFAULT '', lease_until DATETIME NOT NULL,
 material_bytes INTEGER NOT NULL, part_count INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
 reason TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS node_observation_parts (
 intent_id TEXT NOT NULL, ordinal INTEGER NOT NULL, data BLOB NOT NULL,
 PRIMARY KEY (intent_id,ordinal)
);
CREATE TABLE IF NOT EXISTS node_observation_states (
 node_id INTEGER NOT NULL, asset_tag TEXT NOT NULL,
 latest_known INTEGER NOT NULL DEFAULT 0, stats_known INTEGER NOT NULL DEFAULT 0,
 latest_reference TEXT NOT NULL DEFAULT '', stats_reference TEXT NOT NULL DEFAULT '', floor_reference TEXT NOT NULL DEFAULT '',
 deleted INTEGER NOT NULL DEFAULT 0, current_known INTEGER NOT NULL DEFAULT 0, current_source TEXT NOT NULL DEFAULT 'unknown',
 counter_written_stamp TEXT NOT NULL DEFAULT '', counter_written_at DATETIME NULL,
 latest_occurred_at DATETIME NULL, latest_received_at DATETIME NULL, stats_occurred_at DATETIME NULL, stats_received_at DATETIME NULL, updated_at DATETIME NOT NULL,
 PRIMARY KEY (node_id,asset_tag)
);
CREATE INDEX IF NOT EXISTS idx_node_observation_ready ON node_observation_intents(status,available_at,id);
