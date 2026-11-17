CREATE TABLE order_value_budgets (
order_id integer PRIMARY KEY,
user_id integer NOT NULL,
currency varchar(16) NOT NULL,
funding_minor bigint NOT NULL,
refunded_minor bigint NOT NULL DEFAULT 0,
converted_minor bigint NOT NULL DEFAULT 0,
funding_fingerprint varchar(64) NOT NULL,
revision bigint NOT NULL DEFAULT 1,
created_at datetime NOT NULL,
updated_at datetime NOT NULL,
CONSTRAINT ck_value_budget CHECK(funding_minor >= 0 AND refunded_minor >= 0 AND converted_minor >= 0 AND refunded_minor <= funding_minor AND converted_minor <= funding_minor-refunded_minor),
CONSTRAINT fk_order_value_budgets_order FOREIGN KEY(order_id) REFERENCES orders(id) ON DELETE RESTRICT,
CONSTRAINT fk_order_value_budgets_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE RESTRICT
);
CREATE TABLE subscription_value_settlements (
id integer PRIMARY KEY AUTOINCREMENT,
user_id integer NOT NULL,
subscription_id integer NOT NULL,
root_order_id integer NOT NULL,
request_key varchar(64) NOT NULL,
request_fingerprint varchar(64) NOT NULL,
currency varchar(16) NOT NULL,
kind varchar(24) NOT NULL,
amount_minor bigint NOT NULL,
wallet_minor bigint NOT NULL,
applied_minor bigint NOT NULL DEFAULT 0,
entry_id integer DEFAULT NULL,
actor_id integer NOT NULL,
reason varchar(500) NOT NULL,
quote_snapshot text NOT NULL,
created_at datetime NOT NULL,
CONSTRAINT ck_value_settlement_amount CHECK(amount_minor >= 0),
CONSTRAINT ck_value_settlement_split CHECK(wallet_minor >= 0 AND applied_minor >= 0 AND wallet_minor <= amount_minor AND applied_minor = amount_minor-wallet_minor),
CONSTRAINT fk_subscription_value_settlements_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_value_settlements_subscription FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_value_settlements_root_order FOREIGN KEY(root_order_id) REFERENCES orders(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_value_settlements_entry FOREIGN KEY(entry_id) REFERENCES wallet_entries(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_value_settlements_actor FOREIGN KEY(actor_id) REFERENCES users(id) ON DELETE RESTRICT
);
CREATE TABLE subscription_value_settlement_sources (
settlement_id integer NOT NULL,
order_id integer NOT NULL,
amount_minor bigint NOT NULL,
PRIMARY KEY(settlement_id,order_id),
CONSTRAINT ck_value_settlement_source_amount CHECK(amount_minor > 0),
CONSTRAINT fk_subscription_value_settlement_sources_settlement FOREIGN KEY(settlement_id) REFERENCES subscription_value_settlements(id) ON DELETE RESTRICT,
CONSTRAINT fk_subscription_value_settlement_sources_budget FOREIGN KEY(order_id) REFERENCES order_value_budgets(order_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_order_value_budgets_user_id ON order_value_budgets(user_id);
CREATE INDEX IF NOT EXISTS idx_value_settlement_owner ON subscription_value_settlements(user_id,subscription_id,id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_value_settlements_root_order_id ON subscription_value_settlements(root_order_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_value_settlements_request_key ON subscription_value_settlements(request_key);
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_value_settlements_entry_id ON subscription_value_settlements(entry_id);
CREATE INDEX IF NOT EXISTS idx_subscription_value_settlement_sources_order_id ON subscription_value_settlement_sources(order_id);
