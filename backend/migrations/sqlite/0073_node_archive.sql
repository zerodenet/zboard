ALTER TABLE nodes ADD COLUMN archived_at datetime NULL;
CREATE INDEX idx_nodes_archive ON nodes(archived_at,id);
