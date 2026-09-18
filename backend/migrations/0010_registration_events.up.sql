CREATE TABLE IF NOT EXISTS account_registration_events (
    account_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    occurred_at DATETIME(3) NOT NULL,
    processed_at DATETIME(3) NULL,
    task_id BIGINT UNSIGNED NULL,
    INDEX idx_account_registration_events_processed_at (processed_at)
);
