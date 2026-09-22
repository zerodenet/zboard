CREATE TABLE `subscription_mutations` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `subscription_id` bigint unsigned NOT NULL,
  `actor_id` bigint unsigned NOT NULL,
  `kind` varchar(32) NOT NULL,
  `days` int NOT NULL DEFAULT 0,
  `reason` varchar(255) NOT NULL,
  `origin` varchar(191) NOT NULL DEFAULT '',
  `idempotency_key` varchar(128) NOT NULL,
  `request_hash` char(64) NOT NULL,
  `status` varchar(20) NOT NULL,
  `end_at` datetime(3) NOT NULL,
  `created_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_subscription_mutations_idempotency_key` (`idempotency_key`),
  KEY `idx_subscription_mutations_subscription_id` (`subscription_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
