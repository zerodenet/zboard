CREATE TABLE mail_delivery_attempts (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 task_id BIGINT UNSIGNED NOT NULL,
 item_id BIGINT UNSIGNED NOT NULL,
 attempt BIGINT NOT NULL,
 lease_token VARCHAR(191) NOT NULL,
 acceptance VARCHAR(24) NOT NULL,
 started_at DATETIME(3) NOT NULL,
 finished_at DATETIME(3) NULL,
 UNIQUE KEY mail_attempt_item_number (item_id, attempt),
 KEY idx_mail_delivery_attempts_task_id (task_id)
);
