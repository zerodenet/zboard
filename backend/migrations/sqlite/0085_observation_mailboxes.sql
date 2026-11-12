ALTER TABLE node_observation_states ADD COLUMN latest_inline TEXT NOT NULL DEFAULT '';
ALTER TABLE node_observation_states ADD COLUMN stats_inline TEXT NOT NULL DEFAULT '';
CREATE TABLE node_observation_mailboxes (
 node_id INTEGER NOT NULL PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
 asset_tag TEXT NOT NULL,
 observation TEXT NOT NULL CHECK(length(CAST(observation AS BLOB))<=4096),
 pending INTEGER NOT NULL,
 pending_since DATETIME NOT NULL DEFAULT '1970-01-01 00:00:00.000',
 reason TEXT NOT NULL DEFAULT '',
 updated_at DATETIME NOT NULL
);
CREATE INDEX idx_observation_mailbox_pending ON node_observation_mailboxes(pending,pending_since,node_id);
CREATE TABLE node_observation_gc_states (
 id INTEGER NOT NULL PRIMARY KEY,
 after_id TEXT NOT NULL DEFAULT '',
 next_pass_at DATETIME NOT NULL
);
