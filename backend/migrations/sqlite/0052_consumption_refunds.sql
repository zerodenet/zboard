CREATE TABLE consumption_refunds (
 id integer PRIMARY KEY AUTOINCREMENT,
 request_key text NOT NULL,
 fingerprint text NOT NULL,
 order_id integer NOT NULL,
 user_id integer NOT NULL,
 currency text NOT NULL,
 amount_minor integer NOT NULL,
 ticket_id integer NOT NULL,
 actor_id integer NOT NULL,
 reason text NOT NULL,
 entry_id integer NOT NULL,
 created_at datetime NOT NULL,
 CONSTRAINT ck_consumption_refund_amount CHECK(amount_minor > 0),
 CONSTRAINT fk_consumption_refunds_order FOREIGN KEY(order_id) REFERENCES orders(id) ON DELETE RESTRICT,
 CONSTRAINT fk_consumption_refunds_budget FOREIGN KEY(order_id) REFERENCES order_value_budgets(order_id) ON DELETE RESTRICT,
 CONSTRAINT fk_consumption_refunds_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE RESTRICT,
 CONSTRAINT fk_consumption_refunds_actor FOREIGN KEY(actor_id) REFERENCES users(id) ON DELETE RESTRICT,
 CONSTRAINT fk_consumption_refunds_ticket FOREIGN KEY(ticket_id) REFERENCES tickets(id) ON DELETE RESTRICT,
 CONSTRAINT fk_consumption_refunds_entry FOREIGN KEY(entry_id) REFERENCES wallet_entries(id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_consumption_refunds_request_key ON consumption_refunds(request_key);
CREATE UNIQUE INDEX IF NOT EXISTS idx_consumption_refunds_entry_id ON consumption_refunds(entry_id);
CREATE INDEX IF NOT EXISTS idx_consumption_refund_order ON consumption_refunds(order_id,id);
CREATE INDEX IF NOT EXISTS idx_consumption_refunds_user_id ON consumption_refunds(user_id);
CREATE INDEX IF NOT EXISTS idx_consumption_refunds_ticket_id ON consumption_refunds(ticket_id);
CREATE INDEX IF NOT EXISTS idx_consumption_refunds_actor_id ON consumption_refunds(actor_id);
