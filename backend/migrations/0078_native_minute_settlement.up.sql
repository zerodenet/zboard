ALTER TABLE traffic_records ADD COLUMN is_aggregate boolean NOT NULL DEFAULT false, ALGORITHM=INSTANT;
ALTER TABLE traffic_records ADD COLUMN record_count bigint NOT NULL DEFAULT 1, ALGORITHM=INSTANT;
ALTER TABLE metering_usage_projection_pending ADD COLUMN delta_raw_bytes bigint NOT NULL DEFAULT 0, ALGORITHM=INSTANT;
ALTER TABLE metering_usage_projection_pending ADD COLUMN delta_upload_bytes bigint NOT NULL DEFAULT 0, ALGORITHM=INSTANT;
ALTER TABLE metering_usage_projection_pending ADD COLUMN delta_download_bytes bigint NOT NULL DEFAULT 0, ALGORITHM=INSTANT;
ALTER TABLE metering_usage_projection_pending ADD COLUMN delta_used_bytes bigint NOT NULL DEFAULT 0, ALGORITHM=INSTANT;
ALTER TABLE metering_usage_projection_pending ADD COLUMN delta_record_count bigint NOT NULL DEFAULT 0, ALGORITHM=INSTANT;
