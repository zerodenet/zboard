CREATE TABLE traffic_diagnostic_materials (
traffic_record_id bigint unsigned NOT NULL PRIMARY KEY,
source_bytes bigint unsigned NOT NULL,
source_sha256 varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
retired_at datetime(3) NULL DEFAULT NULL,
review_at datetime(3) NOT NULL,
created_at datetime(3) NOT NULL,
CONSTRAINT fk_traffic_diagnostic_record FOREIGN KEY(traffic_record_id) REFERENCES traffic_records(id) ON DELETE RESTRICT ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_traffic_diagnostic_review ON traffic_diagnostic_materials(retired_at,review_at,traffic_record_id);
