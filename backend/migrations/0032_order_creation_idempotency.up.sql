ALTER TABLE orders ADD COLUMN creation_key VARCHAR(64) NULL;
ALTER TABLE orders ADD COLUMN creation_fingerprint VARCHAR(64) NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_orders_creation_key ON orders (creation_key);
