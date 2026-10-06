CREATE TABLE IF NOT EXISTS subscription_alerts (
 id bigint unsigned NOT NULL AUTO_INCREMENT,
 alert_key varchar(64) NOT NULL,
 user_id bigint unsigned NOT NULL,
 subscription_id bigint unsigned NOT NULL,
 kind varchar(24) NOT NULL,
 episode varchar(191) NOT NULL,
 task_id bigint unsigned NOT NULL,
 created_at datetime(3) NOT NULL,
 attempted_at datetime(3) NULL,
 PRIMARY KEY (id),
 UNIQUE KEY idx_subscription_alerts_alert_key (alert_key),
 KEY idx_subscription_alert_user_time (user_id, created_at),
 KEY idx_subscription_alerts_subscription_id (subscription_id),
 KEY idx_subscription_alerts_task_id (task_id),
 KEY idx_subscription_alerts_attempted_at (attempted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS subscription_alert_scans (
 id bigint unsigned NOT NULL,
 last_subscription_id bigint unsigned NOT NULL DEFAULT 0,
 PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
