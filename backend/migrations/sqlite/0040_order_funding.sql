CREATE TABLE order_fundings (
 order_id integer NOT NULL PRIMARY KEY,
 user_id integer NOT NULL,
 currency text NOT NULL,
 use_balance numeric NOT NULL,
 amount_minor integer NOT NULL,
 wallet_minor integer NOT NULL,
 cash_minor integer NOT NULL,
 state text NOT NULL,
 expires_at datetime NOT NULL,
 captured_at datetime,
 released_at datetime,
 release_reason text NOT NULL DEFAULT '',
 created_at datetime NOT NULL,
 updated_at datetime NOT NULL,
 CONSTRAINT ck_order_funding_amounts CHECK(amount_minor >= 0 AND wallet_minor >= 0 AND cash_minor >= 0 AND wallet_minor + cash_minor = amount_minor),
 CONSTRAINT fk_order_fundings_order FOREIGN KEY(order_id) REFERENCES orders(id) ON DELETE RESTRICT,
 CONSTRAINT fk_order_fundings_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_order_fundings_user_id ON order_fundings(user_id);
CREATE INDEX IF NOT EXISTS idx_funding_expiry ON order_fundings(state,expires_at);
