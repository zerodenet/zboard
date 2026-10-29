-- Historical daily projection is retained. Pending rows represent only new
-- authoritative facts and are reliable recovery state, not disposable telemetry.
CREATE TABLE metering_usage_projection_pending (
traffic_record_id integer NOT NULL PRIMARY KEY,
protocol_endpoint_id integer NOT NULL,
created_at datetime NOT NULL,
CONSTRAINT fk_usage_projection_traffic FOREIGN KEY(traffic_record_id) REFERENCES traffic_records(id) ON DELETE RESTRICT
);
CREATE INDEX idx_usage_projection_endpoint ON metering_usage_projection_pending(protocol_endpoint_id,traffic_record_id);
