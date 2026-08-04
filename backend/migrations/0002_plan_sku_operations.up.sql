ALTER TABLE plan_skus
  ADD COLUMN billing_mode varchar(20) NOT NULL DEFAULT 'periodic' AFTER sku_type;

UPDATE plan_skus
   SET billing_mode = CASE
     WHEN billing_unit = 'once' OR sku_type = 'traffic_pack' THEN 'one_time'
     ELSE 'periodic'
   END;

CREATE TABLE plan_sku_operations (
  id bigint unsigned NOT NULL AUTO_INCREMENT,
  plan_sku_id bigint unsigned NOT NULL,
  operation varchar(20) NOT NULL,
  created_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_plan_sku_operations_sku_operation (plan_sku_id, operation),
  KEY idx_plan_sku_operations_operation (operation, plan_sku_id),
  CONSTRAINT fk_plan_sku_operations_sku
    FOREIGN KEY (plan_sku_id) REFERENCES plan_skus (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO plan_sku_operations (plan_sku_id, operation)
SELECT id,
       CASE sku_type
         WHEN 'renewal' THEN 'renew'
         WHEN 'upgrade' THEN 'change'
         WHEN 'traffic_pack' THEN 'addon'
         ELSE 'purchase'
       END
  FROM plan_skus;
