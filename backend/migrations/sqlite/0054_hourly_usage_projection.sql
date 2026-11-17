-- Transactional cutover: existing hourly totals remain untouched. Legacy
-- pending rows default false because their hourly delta was synchronous.
ALTER TABLE metering_usage_projection_pending ADD COLUMN hourly_pending boolean NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN user_id bigint NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN subscription_id bigint NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN node_id bigint NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN record_at datetime DEFAULT NULL;
CREATE INDEX idx_usage_hourly_pending_time ON metering_usage_projection_pending(hourly_pending,record_at);
CREATE INDEX idx_usage_hourly_pending_user ON metering_usage_projection_pending(hourly_pending,user_id,record_at);
CREATE INDEX idx_usage_hourly_pending_subscription ON metering_usage_projection_pending(hourly_pending,subscription_id,record_at);
CREATE INDEX idx_usage_hourly_pending_node ON metering_usage_projection_pending(hourly_pending,node_id,record_at);
CREATE INDEX idx_usage_hourly_pending_endpoint ON metering_usage_projection_pending(hourly_pending,protocol_endpoint_id,record_at);
DROP TRIGGER IF EXISTS traffic_hourly_replace;
DROP TRIGGER IF EXISTS traffic_hourly_insert;
DROP TRIGGER IF EXISTS traffic_hourly_update;
DROP TRIGGER IF EXISTS traffic_hourly_delete;
