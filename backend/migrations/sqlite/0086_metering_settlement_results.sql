ALTER TABLE traffic_records ADD COLUMN result_recorded INTEGER NOT NULL DEFAULT 0;
CREATE TABLE metering_settlement_results (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 result_key TEXT NOT NULL,
 user_id INTEGER NOT NULL,
 subscription_id INTEGER NOT NULL,
 entitlement_epoch INTEGER NOT NULL,
 node_id INTEGER NOT NULL,
 protocol_endpoint_id INTEGER NOT NULL,
 traffic_calc_mode INTEGER NOT NULL,
 protocol_multiplier_milli INTEGER NOT NULL,
 raw_bytes INTEGER NOT NULL,
 upload_bytes INTEGER NOT NULL,
 download_bytes INTEGER NOT NULL,
 used_bytes INTEGER NOT NULL,
 record_count INTEGER NOT NULL
);
CREATE INDEX idx_settlement_result_subscription ON metering_settlement_results(subscription_id);
CREATE TABLE metering_settlement_progress (
 id INTEGER NOT NULL PRIMARY KEY,
 after_id INTEGER NOT NULL,
 upper_id INTEGER NOT NULL
);

CREATE UNIQUE INDEX uk_settlement_result_key ON metering_settlement_results(result_key);
