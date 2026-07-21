ALTER TABLE orders
  ADD COLUMN plan_id BIGINT UNSIGNED NULL AFTER user_id,
  ADD INDEX idx_order_plan (plan_id),
  ADD CONSTRAINT fk_order_plan FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE RESTRICT;
