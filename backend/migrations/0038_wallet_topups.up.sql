ALTER TABLE orders MODIFY COLUMN plan_sku_id bigint unsigned DEFAULT NULL;
CREATE TABLE wallet_accounts (
 user_id bigint unsigned NOT NULL,
 currency varchar(16) NOT NULL,
 balance_minor bigint NOT NULL DEFAULT 0,
 reserved_minor bigint NOT NULL DEFAULT 0,
 revision bigint unsigned NOT NULL DEFAULT 1,
 created_at datetime(3) NOT NULL,
 updated_at datetime(3) NOT NULL,
 PRIMARY KEY(user_id,currency),
 CONSTRAINT fk_wallet_accounts_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE RESTRICT,
 CONSTRAINT ck_wallet_available CHECK(balance_minor >= 0 AND reserved_minor >= 0 AND reserved_minor <= balance_minor)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE wallet_entries (
 id bigint unsigned NOT NULL AUTO_INCREMENT,
 entry_key varchar(64) NOT NULL,
 user_id bigint unsigned NOT NULL,
 currency varchar(16) NOT NULL,
 kind varchar(32) NOT NULL,
 balance_delta bigint NOT NULL,
 reserved_delta bigint NOT NULL DEFAULT 0,
 balance_before bigint NOT NULL,
 balance_after bigint NOT NULL,
 reserved_before bigint NOT NULL DEFAULT 0,
 reserved_after bigint NOT NULL DEFAULT 0,
 source_order_id bigint unsigned DEFAULT NULL,
 reason varchar(500) NOT NULL,
 created_at datetime(3) NOT NULL,
 PRIMARY KEY(id),
 UNIQUE KEY idx_wallet_entries_entry_key(entry_key),
 KEY idx_wallet_entries_account(user_id,currency,id),
 KEY idx_wallet_entries_source_order_id(source_order_id),
 CONSTRAINT fk_wallet_entries_account FOREIGN KEY(user_id,currency) REFERENCES wallet_accounts(user_id,currency) ON DELETE RESTRICT,
 CONSTRAINT fk_wallet_entries_source_order FOREIGN KEY(source_order_id) REFERENCES orders(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
