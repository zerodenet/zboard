-- RunMigrations owns initialization and records0059 only after it succeeds.
-- This column establishes future panel admissions, never historical identity.
ALTER TABLE nodes ADD COLUMN asset_tag varchar(64) NOT NULL DEFAULT '';
