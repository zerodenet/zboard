CREATE TABLE traffic_record_meta_archives (
traffic_record_id bigint unsigned NOT NULL,
version bigint unsigned NOT NULL,
source_bytes bigint unsigned NOT NULL,
source_sha256 varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
fact_sha256 varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
compressed_bytes bigint unsigned NOT NULL,
payload longblob NOT NULL,
created_at datetime(3) NOT NULL,
PRIMARY KEY(traffic_record_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
