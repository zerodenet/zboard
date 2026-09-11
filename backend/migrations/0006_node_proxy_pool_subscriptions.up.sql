ALTER TABLE `node_proxy_pools`
  ADD COLUMN `subscription_url` text NOT NULL,
  ADD COLUMN `subscription_format` varchar(32) NOT NULL DEFAULT '',
  ADD COLUMN `subscription_user_agent` varchar(255) NOT NULL DEFAULT '',
  ADD COLUMN `auto_sync` tinyint(1) NOT NULL DEFAULT 0,
  ADD COLUMN `sync_interval_seconds` int NOT NULL DEFAULT 86400,
  ADD COLUMN `subscription_node_count` int NOT NULL DEFAULT 0,
  ADD COLUMN `last_sync_at` datetime(3) DEFAULT NULL,
  ADD COLUMN `next_sync_at` datetime(3) DEFAULT NULL,
  ADD COLUMN `last_sync_error` varchar(1000) NOT NULL DEFAULT '',
  ADD KEY `idx_node_proxy_pool_sync_due` (`auto_sync`, `next_sync_at`);
