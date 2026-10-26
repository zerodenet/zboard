CREATE TABLE traffic_diagnostic_materials (
traffic_record_id integer NOT NULL PRIMARY KEY,
source_bytes integer NOT NULL,
source_sha256 text NOT NULL,
retired_at datetime NULL DEFAULT NULL,
review_at datetime NOT NULL,
created_at datetime NOT NULL,
CONSTRAINT fk_traffic_diagnostic_record FOREIGN KEY(traffic_record_id) REFERENCES traffic_records(id) ON DELETE RESTRICT ON UPDATE RESTRICT
);
CREATE INDEX idx_traffic_diagnostic_review ON traffic_diagnostic_materials(retired_at,review_at,traffic_record_id);
