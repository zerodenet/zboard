ALTER TABLE order_fundings ADD COLUMN entitlement_minor INTEGER NOT NULL DEFAULT 0 CONSTRAINT ck_order_funding_entitlement CHECK(entitlement_minor >= 0 AND entitlement_minor <= wallet_minor);
