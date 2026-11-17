-- Applied by reconcileAccountSubjectSchema: replace historical owner foreign
-- keys with immutable numeric subjects, preserving all financial evidence.
CREATE TABLE IF NOT EXISTS account_subjects (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY) ENGINE=InnoDB;
