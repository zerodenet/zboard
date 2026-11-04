CREATE TABLE flow_detail_retention_states (
 id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
 upper_id BIGINT UNSIGNED NOT NULL,
 after_id BIGINT UNSIGNED NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_flow_stats_minute_retention ON flow_start_statistics_minutes(minute_at,subscription_id,node_id,basis);
