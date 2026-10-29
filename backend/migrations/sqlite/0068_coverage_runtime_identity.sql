-- SQLite already persists the complete core_instance_id as TEXT in both
-- auxiliary tables. No identity rewrite or coverage reset is required.
SELECT 1;
