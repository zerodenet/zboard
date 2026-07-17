ALTER TABLE orders
  DROP FOREIGN KEY fk_order_plan,
  DROP COLUMN plan_id;
