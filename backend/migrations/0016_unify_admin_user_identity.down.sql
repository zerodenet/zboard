ALTER TABLE users
  ADD COLUMN account_type TINYINT NOT NULL DEFAULT 3 AFTER password;

UPDATE users
SET account_type = IF(is_admin, 1, 3);
