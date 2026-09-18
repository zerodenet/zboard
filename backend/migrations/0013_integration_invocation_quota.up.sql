ALTER TABLE user_api_tokens ADD COLUMN invocation_window_started_at DATETIME(3) NULL;
ALTER TABLE user_api_tokens ADD COLUMN invocation_window_count BIGINT UNSIGNED NOT NULL DEFAULT 0;
CREATE INDEX idx_user_api_tokens_invocation_window ON user_api_tokens(invocation_window_started_at);
