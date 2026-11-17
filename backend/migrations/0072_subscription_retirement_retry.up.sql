-- Retry scheduling is operational state, not financial evidence. A missing
-- column may be restored as NULL (eligible immediately) by owned preparation.
ALTER TABLE subscriptions ADD COLUMN retirement_retry_at DATETIME(3) NULL DEFAULT NULL;
CREATE INDEX idx_subscriptions_retirement_retry ON subscriptions(retirement_retry_at, id);
