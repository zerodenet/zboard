UPDATE users
SET is_admin = TRUE
WHERE account_type = 1;

ALTER TABLE users
  DROP COLUMN account_type;
