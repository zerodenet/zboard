ALTER TABLE order_fundings ADD COLUMN entitlement_minor bigint NOT NULL DEFAULT 0;
ALTER TABLE order_fundings ADD CONSTRAINT ck_order_funding_entitlement CHECK(entitlement_minor >= 0 AND entitlement_minor <= wallet_minor);
ALTER TABLE orders MODIFY COLUMN change_snapshot LONGTEXT NOT NULL DEFAULT ('');
