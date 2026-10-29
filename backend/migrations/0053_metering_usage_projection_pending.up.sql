-- Existing daily rows already cover historical ledger entries. Queue only facts
-- settled by the new writer; do not replay older facts into the additive rollup.
CREATE TABLE metering_usage_projection_pending (
traffic_record_id bigint unsigned NOT NULL PRIMARY KEY,
protocol_endpoint_id bigint unsigned NOT NULL,
created_at datetime(3) NOT NULL,
KEY idx_usage_projection_endpoint(protocol_endpoint_id,traffic_record_id),
CONSTRAINT fk_usage_projection_traffic FOREIGN KEY(traffic_record_id) REFERENCES traffic_records(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
