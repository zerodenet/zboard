CREATE TABLE subscription_automatic_renewals (
subscription_id integer PRIMARY KEY,
user_id integer NOT NULL,
enabled integer NOT NULL DEFAULT 0,
plan_sku_id integer NOT NULL,
root_order_id integer NOT NULL,
agreement_digest varchar(64) NOT NULL,
revision bigint NOT NULL DEFAULT 1,
state varchar(32) NOT NULL,
message varchar(500) NOT NULL DEFAULT '',
last_order_id integer DEFAULT NULL,
last_attempt_at datetime DEFAULT NULL,
attempted_end_at datetime DEFAULT NULL,
attempt_sequence bigint unsigned NOT NULL DEFAULT 0,
next_attempt_at datetime NOT NULL,
created_at datetime NOT NULL,
updated_at datetime NOT NULL,
CONSTRAINT fk_subscription_automatic_renewals_subscription FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_automatic_renewals_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_automatic_renewals_sku FOREIGN KEY(plan_sku_id) REFERENCES plan_skus(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_automatic_renewals_root_order FOREIGN KEY(root_order_id) REFERENCES orders(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_automatic_renewals_last_order FOREIGN KEY(last_order_id) REFERENCES orders(id) ON DELETE RESTRICT
);
CREATE TABLE automatic_renewal_scans (
id integer PRIMARY KEY,
sequence bigint unsigned NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_automatic_renewal_due ON subscription_automatic_renewals(enabled,next_attempt_at,user_id);
INSERT INTO automatic_renewal_scans(id,sequence) VALUES(1,0) ON CONFLICT(id) DO NOTHING;
