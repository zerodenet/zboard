CREATE TABLE IF NOT EXISTS external_identities (
 id VARCHAR(64) PRIMARY KEY,
 user_id BIGINT UNSIGNED NOT NULL,
 plugin_id VARCHAR(160) NOT NULL,
 publisher VARCHAR(160) NOT NULL,
 issuer TEXT NOT NULL,
 subject TEXT NOT NULL,
 created_at DATETIME(3) NOT NULL,
 UNIQUE KEY idx_external_identity_user_plugin (user_id, plugin_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
