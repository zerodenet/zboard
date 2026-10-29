CREATE TABLE IF NOT EXISTS flow_start_statistics_state(id INTEGER NOT NULL PRIMARY KEY,legacy_upper_id INTEGER NOT NULL,legacy_after_id INTEGER NOT NULL,legacy_expected_rows INTEGER NOT NULL,legacy_projected_rows INTEGER NOT NULL,projected_head INTEGER NOT NULL,revision INTEGER NOT NULL,created_at DATETIME NOT NULL,updated_at DATETIME NOT NULL);
CREATE TABLE IF NOT EXISTS flow_start_statistics_pending(source_id INTEGER NOT NULL PRIMARY KEY,subscription_id INTEGER NOT NULL,node_id INTEGER NOT NULL,occurred_at DATETIME NOT NULL,received_at DATETIME NOT NULL,created_at DATETIME NOT NULL);
CREATE INDEX IF NOT EXISTS idx_flow_stats_pending_received ON flow_start_statistics_pending(subscription_id,received_at,source_id);
CREATE INDEX IF NOT EXISTS idx_flow_stats_pending_occurred ON flow_start_statistics_pending(subscription_id,occurred_at,source_id);
CREATE TABLE IF NOT EXISTS flow_start_statistics_points(source_id INTEGER NOT NULL PRIMARY KEY,subscription_id INTEGER NOT NULL,node_id INTEGER NOT NULL,occurred_at DATETIME NOT NULL,received_at DATETIME NOT NULL,source_created_at DATETIME NOT NULL,review_at DATETIME NOT NULL,fact_sha256 TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_flow_stats_point_retention ON flow_start_statistics_points(review_at,source_id);
CREATE INDEX IF NOT EXISTS idx_flow_stats_point_received ON flow_start_statistics_points(subscription_id,received_at,node_id,source_id);
CREATE INDEX IF NOT EXISTS idx_flow_stats_point_occurred ON flow_start_statistics_points(subscription_id,occurred_at,node_id,source_id);
CREATE TABLE IF NOT EXISTS flow_start_statistics_minutes(subscription_id INTEGER NOT NULL,node_id INTEGER NOT NULL,basis TEXT NOT NULL,minute_at DATETIME NOT NULL,event_count INTEGER NOT NULL,PRIMARY KEY(subscription_id,node_id,basis,minute_at));
CREATE INDEX IF NOT EXISTS idx_flow_stats_minute_read ON flow_start_statistics_minutes(subscription_id,basis,minute_at,node_id);

-- Owned runner verifies partial-column retries before skipping this ALTER.
ALTER TABLE subscription_flow_start_events ADD COLUMN statistics_registered BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX idx_flow_start_statistics_registered ON subscription_flow_start_events(statistics_registered,id);
