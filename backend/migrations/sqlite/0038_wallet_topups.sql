CREATE TABLE wallet_accounts (
 user_id INTEGER NOT NULL,
 currency TEXT NOT NULL,
 balance_minor INTEGER NOT NULL DEFAULT 0,
 reserved_minor INTEGER NOT NULL DEFAULT 0,
 revision INTEGER NOT NULL DEFAULT 1,
 created_at DATETIME NOT NULL,
 updated_at DATETIME NOT NULL,
 PRIMARY KEY(user_id,currency),
 CONSTRAINT fk_wallet_accounts_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE RESTRICT,
 CONSTRAINT ck_wallet_available CHECK(balance_minor >= 0 AND reserved_minor >= 0 AND reserved_minor <= balance_minor)
);
CREATE TABLE wallet_entries (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 entry_key TEXT NOT NULL,
 user_id INTEGER NOT NULL,
 currency TEXT NOT NULL,
 kind TEXT NOT NULL,
 balance_delta INTEGER NOT NULL,
 reserved_delta INTEGER NOT NULL DEFAULT 0,
 balance_before INTEGER NOT NULL,
 balance_after INTEGER NOT NULL,
 reserved_before INTEGER NOT NULL DEFAULT 0,
 reserved_after INTEGER NOT NULL DEFAULT 0,
 source_order_id INTEGER DEFAULT NULL,
 reason TEXT NOT NULL,
 created_at DATETIME NOT NULL,
 UNIQUE(entry_key),
 CONSTRAINT fk_wallet_entries_account FOREIGN KEY(user_id,currency) REFERENCES wallet_accounts(user_id,currency) ON DELETE RESTRICT,
 CONSTRAINT fk_wallet_entries_source_order FOREIGN KEY(source_order_id) REFERENCES orders(id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_wallet_entries_account ON wallet_entries(user_id,currency,id);
CREATE INDEX IF NOT EXISTS idx_wallet_entries_source_order_id ON wallet_entries(source_order_id);
