CREATE TABLE metering_report_receipts (
node_id integer NOT NULL,
report_id text COLLATE BINARY NOT NULL,
protocol_endpoint_id integer NOT NULL,
traffic_record_id integer NOT NULL,
created_at datetime NOT NULL,
PRIMARY KEY(node_id,report_id)
);
