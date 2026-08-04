DROP TABLE IF EXISTS plan_sku_operations;

ALTER TABLE plan_skus
  DROP COLUMN billing_mode;
