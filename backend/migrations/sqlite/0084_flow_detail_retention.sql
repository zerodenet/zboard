CREATE TABLE flow_detail_retention_states (
 id INTEGER NOT NULL PRIMARY KEY,
 upper_id INTEGER NOT NULL,
 after_id INTEGER NOT NULL
);
CREATE INDEX idx_flow_stats_minute_retention ON flow_start_statistics_minutes(minute_at,subscription_id,node_id,basis);
