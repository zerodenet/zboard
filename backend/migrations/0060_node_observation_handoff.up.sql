-- Owned reconciliation imports legacy positions only as unattributed floors.
CREATE TABLE IF NOT EXISTS node_observation_intents (
 id varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY, node_id bigint unsigned NOT NULL, asset_tag varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 status varchar(24) NOT NULL, available_at datetime(3) NOT NULL, token varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '', lease_until datetime(3) NOT NULL,
 material_bytes bigint unsigned NOT NULL, part_count bigint unsigned NOT NULL, attempts bigint unsigned NOT NULL DEFAULT 0,
 reason varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '', created_at datetime(3) NOT NULL, updated_at datetime(3) NOT NULL,
 KEY idx_node_observation_ready (status,available_at,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS node_observation_parts (
 intent_id varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL, ordinal bigint unsigned NOT NULL, data blob NOT NULL,
 PRIMARY KEY (intent_id,ordinal)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS node_observation_states (
 node_id bigint unsigned NOT NULL, asset_tag varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 latest_known tinyint(1) NOT NULL DEFAULT 0, stats_known tinyint(1) NOT NULL DEFAULT 0,
 latest_reference varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '', stats_reference varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '', floor_reference varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
 deleted tinyint(1) NOT NULL DEFAULT 0, current_known tinyint(1) NOT NULL DEFAULT 0, current_source varchar(32) NOT NULL DEFAULT 'unknown',
 counter_written_stamp varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '', counter_written_at datetime(3) NULL,
 latest_occurred_at datetime(3) NULL, latest_received_at datetime(3) NULL, stats_occurred_at datetime(3) NULL, stats_received_at datetime(3) NULL, updated_at datetime(3) NOT NULL,
 PRIMARY KEY (node_id,asset_tag)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
