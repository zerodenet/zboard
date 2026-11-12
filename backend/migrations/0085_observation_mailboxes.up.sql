ALTER TABLE node_observation_states ADD COLUMN latest_inline TEXT NOT NULL DEFAULT ('');
ALTER TABLE node_observation_states ADD COLUMN stats_inline TEXT NOT NULL DEFAULT ('');
CREATE TABLE node_observation_mailboxes (
 node_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
 asset_tag VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 observation TEXT NOT NULL,
 pending BOOLEAN NOT NULL,
 pending_since DATETIME(3) NOT NULL DEFAULT '1970-01-01 00:00:00.000',
 reason VARCHAR(64) NOT NULL DEFAULT '',
 updated_at DATETIME(3) NOT NULL,
 INDEX idx_observation_mailbox_pending(pending,pending_since,node_id),
 CONSTRAINT fk_observation_mailbox_node FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE,
 CONSTRAINT ck_observation_mailbox_bytes CHECK(OCTET_LENGTH(observation)<=4096)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE node_observation_gc_states (
 id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
 after_id VARCHAR(64) NOT NULL DEFAULT '',
 next_pass_at DATETIME(3) NOT NULL
);
