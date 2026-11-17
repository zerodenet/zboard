ALTER TABLE subscriptions ADD COLUMN kernel_policy_revision BIGINT UNSIGNED NOT NULL DEFAULT 0;
ALTER TABLE subscriptions ADD COLUMN kernel_policy_fingerprint VARCHAR(64) NOT NULL DEFAULT '';
