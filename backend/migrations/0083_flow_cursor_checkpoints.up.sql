CREATE TABLE flow_cursor_checkpoints (
 node_id BIGINT UNSIGNED NOT NULL,
 flow_id VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL,
 protocol_credential_id BIGINT UNSIGNED NOT NULL,
 revision BIGINT UNSIGNED NOT NULL,
 raw_bytes BIGINT NOT NULL,
 upload_bytes BIGINT NOT NULL,
 download_bytes BIGINT NOT NULL,
 used_bytes BIGINT NOT NULL,
 is_completed BOOLEAN NOT NULL,
 PRIMARY KEY (node_id, flow_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE flow_cursor_compaction_states (
 id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
 after_id BIGINT UNSIGNED NOT NULL,
 next_pass_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
