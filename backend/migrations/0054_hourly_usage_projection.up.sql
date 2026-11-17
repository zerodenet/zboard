-- Stop all old synchronous writers before this cutover. Existing 0053 intents
-- already have their hourly delta; false is evidence, never replay them twice.
ALTER TABLE metering_usage_projection_pending ADD COLUMN hourly_pending boolean NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN user_id bigint unsigned NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN subscription_id bigint unsigned NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN node_id bigint unsigned NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN record_at datetime(3) DEFAULT NULL;
CREATE INDEX idx_usage_hourly_pending_time ON metering_usage_projection_pending(hourly_pending,record_at);
CREATE INDEX idx_usage_hourly_pending_user ON metering_usage_projection_pending(hourly_pending,user_id,record_at);
CREATE INDEX idx_usage_hourly_pending_subscription ON metering_usage_projection_pending(hourly_pending,subscription_id,record_at);
CREATE INDEX idx_usage_hourly_pending_node ON metering_usage_projection_pending(hourly_pending,node_id,record_at);
CREATE INDEX idx_usage_hourly_pending_endpoint ON metering_usage_projection_pending(hourly_pending,protocol_endpoint_id,record_at);
-- Remove any restored legacy 0024 writer as well; metadata visibility alone
-- cannot prove that a trigger is absent on an unprivileged migration account.
DROP TRIGGER IF EXISTS traffic_hourly_insert;
DROP TRIGGER IF EXISTS traffic_hourly_update_remove;
DROP TRIGGER IF EXISTS traffic_hourly_update_add;
DROP TRIGGER IF EXISTS traffic_hourly_delete;
