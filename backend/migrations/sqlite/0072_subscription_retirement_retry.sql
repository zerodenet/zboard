ALTER TABLE subscriptions ADD COLUMN retirement_retry_at DATETIME NULL DEFAULT NULL;
CREATE INDEX idx_subscriptions_retirement_retry ON subscriptions(retirement_retry_at, id);
