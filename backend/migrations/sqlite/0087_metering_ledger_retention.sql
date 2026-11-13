ALTER TABLE traffic_records ADD COLUMN retention_level INTEGER NOT NULL DEFAULT 0;
CREATE TABLE metering_nonce_receipts (
 node_id INTEGER NOT NULL,
 nonce TEXT NOT NULL,
 report_id TEXT NOT NULL,
 PRIMARY KEY(node_id,nonce)
);
CREATE TABLE metering_ledger_retention_progress (
 high_water_id INTEGER NOT NULL DEFAULT 0,
 held_count INTEGER NOT NULL DEFAULT 0,
 last_held_id INTEGER NOT NULL DEFAULT 0,
 last_hold_reason TEXT NOT NULL DEFAULT '',
 id INTEGER NOT NULL PRIMARY KEY,
 after_id INTEGER NOT NULL,
 upper_id INTEGER NOT NULL,
 next_pass_at DATETIME NOT NULL
);
CREATE TABLE metering_ledger_targets (source_id INTEGER NOT NULL PRIMARY KEY,node_id INTEGER NOT NULL,protocol_endpoint_id INTEGER NOT NULL,record_id INTEGER NOT NULL,result_id INTEGER NOT NULL);
