CREATE TABLE IF NOT EXISTS protocol_endpoint_usage_resets (
 id bigint unsigned NOT NULL AUTO_INCREMENT,
 protocol_endpoint_id bigint unsigned NOT NULL,
 reset_at datetime(3) NOT NULL,
 usage_date date NOT NULL,
 baseline_total_bytes bigint NOT NULL,
 baseline_today_bytes bigint NOT NULL,
 period_used_bytes bigint NOT NULL,
 reason varchar(255) NOT NULL,
 actor_user_id bigint unsigned NOT NULL,
 PRIMARY KEY (id),
 KEY idx_protocol_endpoint_usage_resets_endpoint (protocol_endpoint_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
