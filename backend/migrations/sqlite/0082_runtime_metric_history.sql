CREATE TABLE runtime_metric_minutes (
 slot INTEGER NOT NULL PRIMARY KEY,
 minute_at DATETIME NOT NULL,
 sample_count INTEGER NOT NULL,
 cpu_sum REAL NOT NULL, memory_sum REAL NOT NULL, disk_sum REAL NOT NULL, rx_sum REAL NOT NULL, tx_sum REAL NOT NULL,
 cpu_count INTEGER NOT NULL, memory_count INTEGER NOT NULL, disk_count INTEGER NOT NULL, rx_count INTEGER NOT NULL, tx_count INTEGER NOT NULL,
 CONSTRAINT chk_runtime_metric_slot CHECK (slot >= 0 AND slot < 1440)
);
CREATE INDEX idx_runtime_metric_expiry ON runtime_metric_minutes (minute_at);
