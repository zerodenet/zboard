ALTER TABLE traffic_records ADD COLUMN is_aggregate numeric NOT NULL DEFAULT false;
ALTER TABLE traffic_records ADD COLUMN record_count integer NOT NULL DEFAULT 1;
ALTER TABLE metering_usage_projection_pending ADD COLUMN delta_raw_bytes integer NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN delta_upload_bytes integer NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN delta_download_bytes integer NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN delta_used_bytes integer NOT NULL DEFAULT 0;
ALTER TABLE metering_usage_projection_pending ADD COLUMN delta_record_count integer NOT NULL DEFAULT 0;
