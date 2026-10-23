CREATE TABLE payment_attempts (
 id integer PRIMARY KEY AUTOINCREMENT,
 order_id integer NOT NULL,
 provider text NOT NULL,
 amount_minor integer NOT NULL,
 currency text NOT NULL,
 created_at datetime NOT NULL,
 CONSTRAINT fk_payment_attempts_order FOREIGN KEY(order_id) REFERENCES orders(id) ON DELETE RESTRICT
);
CREATE TABLE payment_receipts (
 receipt_key text NOT NULL,
 id integer PRIMARY KEY AUTOINCREMENT,
 provider text NOT NULL,
 provider_trade_no text NOT NULL,
 order_id integer NOT NULL,
 amount_minor integer NOT NULL,
 currency text NOT NULL,
 disposition text NOT NULL,
 wallet_entry_id integer,
 created_at datetime NOT NULL,
 processed_at datetime NOT NULL,
 resolved_by integer,
 ticket_id integer,
 resolution_reason text NOT NULL DEFAULT '',
 resolved_at datetime,
 CONSTRAINT fk_payment_receipts_resolver FOREIGN KEY(resolved_by) REFERENCES users(id) ON DELETE RESTRICT,
 CONSTRAINT fk_payment_receipts_ticket FOREIGN KEY(ticket_id) REFERENCES tickets(id) ON DELETE RESTRICT,
 CONSTRAINT fk_payment_receipts_order FOREIGN KEY(order_id) REFERENCES orders(id) ON DELETE RESTRICT,
 CONSTRAINT fk_payment_receipts_wallet_entry FOREIGN KEY(wallet_entry_id) REFERENCES wallet_entries(id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_payment_attempt ON payment_attempts(order_id,provider);
CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_receipts_receipt_key ON payment_receipts(receipt_key);
CREATE INDEX IF NOT EXISTS idx_payment_receipts_order_id ON payment_receipts(order_id);
CREATE INDEX IF NOT EXISTS idx_payment_receipts_wallet_entry_id ON payment_receipts(wallet_entry_id);
CREATE INDEX IF NOT EXISTS idx_payment_receipts_resolved_by ON payment_receipts(resolved_by);
CREATE INDEX IF NOT EXISTS idx_payment_receipts_ticket_id ON payment_receipts(ticket_id);
