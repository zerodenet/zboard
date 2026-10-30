ALTER TABLE protocol_endpoints ADD COLUMN deleted_at datetime(3) NULL;
CREATE INDEX idx_protocol_endpoints_deletion ON protocol_endpoints(deleted_at,id);
