CREATE TABLE wallet_adjustments (
 id integer PRIMARY KEY AUTOINCREMENT,
 request_key text NOT NULL,
 fingerprint text NOT NULL,
 user_id integer NOT NULL,
 currency text NOT NULL,
 amount_minor integer NOT NULL,
 reason text NOT NULL,
 ticket_id integer NOT NULL,
 actor_id integer NOT NULL,
 entry_id integer NOT NULL,
 reversal_entry_id integer DEFAULT NULL,
 reversal_actor_id integer DEFAULT NULL,
 reversal_reason text NOT NULL DEFAULT '',
 external_status text NOT NULL DEFAULT 'pending',
 created_at datetime NOT NULL,
 updated_at datetime NOT NULL,
 CONSTRAINT ck_wallet_adjustment_amount CHECK(amount_minor > 0),
 CONSTRAINT fk_wallet_adjustments_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE RESTRICT,
 CONSTRAINT fk_wallet_adjustments_actor FOREIGN KEY(actor_id) REFERENCES users(id) ON DELETE RESTRICT,
 CONSTRAINT fk_wallet_adjustments_reversal_actor FOREIGN KEY(reversal_actor_id) REFERENCES users(id) ON DELETE RESTRICT,
 CONSTRAINT fk_wallet_adjustments_ticket FOREIGN KEY(ticket_id) REFERENCES tickets(id) ON DELETE RESTRICT,
 CONSTRAINT fk_wallet_adjustments_entry FOREIGN KEY(entry_id) REFERENCES wallet_entries(id) ON DELETE RESTRICT,
 CONSTRAINT fk_wallet_adjustments_reversal_entry FOREIGN KEY(reversal_entry_id) REFERENCES wallet_entries(id) ON DELETE RESTRICT
);
CREATE TABLE wallet_payout_results (
 id integer PRIMARY KEY AUTOINCREMENT,
 adjustment_id integer NOT NULL,
 request_key text NOT NULL,
 fingerprint text NOT NULL,
 actor_id integer NOT NULL,
 outcome text NOT NULL,
 reference text NOT NULL DEFAULT '',
 note text NOT NULL DEFAULT '',
 occurred_at datetime NOT NULL,
 created_at datetime NOT NULL,
 CONSTRAINT fk_wallet_payout_results_adjustment FOREIGN KEY(adjustment_id) REFERENCES wallet_adjustments(id) ON DELETE RESTRICT,
 CONSTRAINT fk_wallet_payout_results_actor FOREIGN KEY(actor_id) REFERENCES users(id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_wallet_adjustments_request_key ON wallet_adjustments(request_key);
CREATE UNIQUE INDEX IF NOT EXISTS idx_wallet_adjustments_entry_id ON wallet_adjustments(entry_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_wallet_adjustments_reversal_entry_id ON wallet_adjustments(reversal_entry_id);
CREATE INDEX IF NOT EXISTS idx_wallet_adjustment_owner ON wallet_adjustments(user_id,currency,id);
CREATE INDEX IF NOT EXISTS idx_wallet_adjustments_ticket_id ON wallet_adjustments(ticket_id);
CREATE INDEX IF NOT EXISTS idx_wallet_adjustments_actor_id ON wallet_adjustments(actor_id);
CREATE INDEX IF NOT EXISTS idx_wallet_adjustments_reversal_actor_id ON wallet_adjustments(reversal_actor_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_wallet_payout_results_request_key ON wallet_payout_results(request_key);
CREATE INDEX IF NOT EXISTS idx_wallet_payout_history ON wallet_payout_results(adjustment_id,id);
CREATE INDEX IF NOT EXISTS idx_wallet_payout_results_actor_id ON wallet_payout_results(actor_id);
