CREATE TABLE principal_statistics_minutes (
 scope_type varchar(24) COLLATE utf8mb4_unicode_ci NOT NULL,
 scope_id bigint unsigned NOT NULL,
 minute_at datetime(3) NOT NULL,
 peak bigint unsigned NOT NULL,
 sample_count bigint unsigned NOT NULL,
 PRIMARY KEY(scope_type,scope_id,minute_at),
 KEY idx_principal_minute_retention(minute_at,scope_type,scope_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
