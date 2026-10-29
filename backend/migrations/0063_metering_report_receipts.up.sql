CREATE TABLE metering_report_receipts (
node_id bigint unsigned NOT NULL,
report_id varchar(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL,
protocol_endpoint_id bigint unsigned NOT NULL,
traffic_record_id bigint unsigned NOT NULL,
created_at datetime(3) NOT NULL,
PRIMARY KEY(node_id,report_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
