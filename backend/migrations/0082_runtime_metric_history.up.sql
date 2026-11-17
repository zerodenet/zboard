CREATE TABLE runtime_metric_minutes (
 slot SMALLINT UNSIGNED NOT NULL PRIMARY KEY,
 minute_at DATETIME(3) NOT NULL,
 sample_count BIGINT UNSIGNED NOT NULL,
 cpu_sum DOUBLE NOT NULL, memory_sum DOUBLE NOT NULL, disk_sum DOUBLE NOT NULL, rx_sum DOUBLE NOT NULL, tx_sum DOUBLE NOT NULL,
 cpu_count INT UNSIGNED NOT NULL, memory_count INT UNSIGNED NOT NULL, disk_count INT UNSIGNED NOT NULL, rx_count INT UNSIGNED NOT NULL, tx_count INT UNSIGNED NOT NULL,
 CONSTRAINT chk_runtime_metric_slot CHECK (slot < 1440),
 KEY idx_runtime_metric_expiry (minute_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
