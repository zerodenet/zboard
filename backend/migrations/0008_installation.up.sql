CREATE TABLE IF NOT EXISTS installations (
  id BIGINT UNSIGNED NOT NULL,
  site_name VARCHAR(80) NOT NULL,
  site_url VARCHAR(255) NOT NULL,
  allow_registration TINYINT(1) NOT NULL DEFAULT 1,
  installed_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
