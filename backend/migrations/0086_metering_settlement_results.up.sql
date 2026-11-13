ALTER TABLE traffic_records ADD COLUMN result_recorded BOOLEAN NOT NULL DEFAULT FALSE, ALGORITHM=INSTANT;
CREATE TABLE metering_settlement_results (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 result_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 UNIQUE KEY uk_settlement_result_key(result_key),
 user_id BIGINT UNSIGNED NOT NULL,
 subscription_id BIGINT UNSIGNED NOT NULL,
 entitlement_epoch BIGINT UNSIGNED NOT NULL,
 node_id BIGINT UNSIGNED NOT NULL,
 protocol_endpoint_id BIGINT UNSIGNED NOT NULL,
 traffic_calc_mode SMALLINT NOT NULL,
 protocol_multiplier_milli BIGINT NOT NULL,
 raw_bytes BIGINT NOT NULL,
 upload_bytes BIGINT NOT NULL,
 download_bytes BIGINT NOT NULL,
 used_bytes BIGINT NOT NULL,
 record_count BIGINT NOT NULL,
 INDEX idx_settlement_result_subscription(subscription_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE metering_settlement_progress (
 id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
 after_id BIGINT UNSIGNED NOT NULL,
 upper_id BIGINT UNSIGNED NOT NULL
) ENGINE=InnoDB;
