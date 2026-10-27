CREATE TABLE order_discount_redemptions (
order_id integer NOT NULL PRIMARY KEY,
redemption_key text NOT NULL,
binding_hash text NOT NULL,
binding_json text NOT NULL,
provider_id text NOT NULL,
desired text NOT NULL,
observed text NOT NULL,
token text NOT NULL,
revision integer NOT NULL,
lease_token text NOT NULL,
lease_until datetime NOT NULL,
next_attempt_at datetime NOT NULL,
last_error text NOT NULL,
created_at datetime NOT NULL,
updated_at datetime NOT NULL
);
CREATE UNIQUE INDEX idx_order_discount_redemption_key ON order_discount_redemptions(redemption_key);
CREATE INDEX idx_order_discount_redemption_due ON order_discount_redemptions(next_attempt_at, order_id);

CREATE INDEX idx_order_discount_redemption_expiry ON order_discount_redemptions(desired, created_at, order_id);
