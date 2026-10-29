CREATE TABLE subscription_automatic_renewals (
subscription_id bigint unsigned PRIMARY KEY,
user_id bigint unsigned NOT NULL,
enabled boolean NOT NULL DEFAULT 0,
plan_sku_id bigint unsigned NOT NULL,
root_order_id bigint unsigned NOT NULL,
agreement_digest varchar(64) NOT NULL,
revision bigint NOT NULL DEFAULT 1,
state varchar(32) NOT NULL,
message varchar(500) NOT NULL DEFAULT '',
last_order_id bigint unsigned DEFAULT NULL,
last_attempt_at datetime(3) DEFAULT NULL,
attempted_end_at datetime(3) DEFAULT NULL,
attempt_sequence bigint unsigned NOT NULL DEFAULT 0,
next_attempt_at datetime(3) NOT NULL,
created_at datetime(3) NOT NULL,
updated_at datetime(3) NOT NULL,
CONSTRAINT fk_subscription_automatic_renewals_subscription FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_automatic_renewals_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_automatic_renewals_sku FOREIGN KEY(plan_sku_id) REFERENCES plan_skus(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_automatic_renewals_root_order FOREIGN KEY(root_order_id) REFERENCES orders(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_automatic_renewals_last_order FOREIGN KEY(last_order_id) REFERENCES orders(id) ON DELETE RESTRICT,
KEY idx_automatic_renewal_due(enabled,next_attempt_at,user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE automatic_renewal_scans (
id bigint unsigned PRIMARY KEY,
sequence bigint unsigned NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT INTO automatic_renewal_scans(id,sequence) VALUES(1,0) ON DUPLICATE KEY UPDATE id=VALUES(id);
