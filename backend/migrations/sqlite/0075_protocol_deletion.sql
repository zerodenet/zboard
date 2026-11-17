ALTER TABLE protocol_endpoints ADD COLUMN deleted_at datetime NULL;
CREATE INDEX idx_protocol_endpoints_deletion ON protocol_endpoints(deleted_at,id);
