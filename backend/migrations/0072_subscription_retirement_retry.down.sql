-- Stop lifecycle workers before rollback. This removes only retry scheduling;
-- orders, funding, credentials, publication intents and audit remain intact.
DROP INDEX idx_subscriptions_retirement_retry ON subscriptions;
ALTER TABLE subscriptions DROP COLUMN retirement_retry_at;
