ALTER TABLE subscription_automatic_renewals ADD COLUMN pending_order_id bigint unsigned NULL;
ALTER TABLE subscription_automatic_renewals ADD COLUMN pending_consent_revision bigint unsigned NOT NULL DEFAULT 0;
ALTER TABLE subscription_automatic_renewals ADD COLUMN pending_end_at datetime(3) NULL;
ALTER TABLE subscription_automatic_renewals ADD COLUMN pending_epoch bigint unsigned NOT NULL DEFAULT 0;
ALTER TABLE subscription_automatic_renewals ADD COLUMN pending_attempt bigint unsigned NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX idx_auto_pending_order ON subscription_automatic_renewals(pending_order_id);
CREATE INDEX idx_auto_pending_due ON subscription_automatic_renewals(pending_order_id,next_attempt_at,user_id);
