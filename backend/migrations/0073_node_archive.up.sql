ALTER TABLE nodes ADD COLUMN archived_at datetime(3) NULL;
CREATE INDEX idx_nodes_archive ON nodes(archived_at,id);
