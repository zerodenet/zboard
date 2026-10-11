DROP INDEX idx_orders_creation_key ON orders;
ALTER TABLE orders DROP COLUMN creation_fingerprint;
ALTER TABLE orders DROP COLUMN creation_key;
