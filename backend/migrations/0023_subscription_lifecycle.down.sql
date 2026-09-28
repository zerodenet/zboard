DROP INDEX idx_subscriptions_ended_at ON subscriptions;
ALTER TABLE orders DROP COLUMN subscription_final_flow_used;
ALTER TABLE orders DROP COLUMN subscription_final_flow_total;
ALTER TABLE orders DROP COLUMN subscription_end_reason;
ALTER TABLE orders DROP COLUMN subscription_ended_at;
ALTER TABLE subscriptions DROP COLUMN end_reason;
ALTER TABLE subscriptions DROP COLUMN ended_at;
ALTER TABLE subscriptions DROP COLUMN ends_on_quota_exhaustion;
ALTER TABLE subscriptions DROP COLUMN lifecycle;
