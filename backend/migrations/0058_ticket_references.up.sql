ALTER TABLE tickets
  ADD COLUMN order_id bigint unsigned NULL DEFAULT NULL,
  ADD COLUMN subscription_id bigint unsigned NULL DEFAULT NULL,
  ADD COLUMN wallet_entry_id bigint unsigned NULL DEFAULT NULL;
