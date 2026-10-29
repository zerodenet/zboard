CREATE TABLE traffic_record_meta_archives (
traffic_record_id integer NOT NULL PRIMARY KEY,
version integer NOT NULL,
source_bytes integer NOT NULL,
source_sha256 text COLLATE BINARY NOT NULL,
fact_sha256 text COLLATE BINARY NOT NULL,
compressed_bytes integer NOT NULL,
payload blob NOT NULL,
created_at datetime NOT NULL
);
