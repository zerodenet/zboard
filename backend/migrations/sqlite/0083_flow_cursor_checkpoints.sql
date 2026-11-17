CREATE TABLE flow_cursor_checkpoints (
 node_id INTEGER NOT NULL,
 flow_id TEXT COLLATE BINARY NOT NULL,
 protocol_credential_id INTEGER NOT NULL,
 revision INTEGER NOT NULL,
 raw_bytes INTEGER NOT NULL,
 upload_bytes INTEGER NOT NULL,
 download_bytes INTEGER NOT NULL,
 used_bytes INTEGER NOT NULL,
 is_completed NUMERIC NOT NULL,
 PRIMARY KEY (node_id, flow_id)
);
CREATE TABLE flow_cursor_compaction_states (
 id INTEGER NOT NULL PRIMARY KEY,
 after_id INTEGER NOT NULL,
 next_pass_at DATETIME NOT NULL
);
