-- Retain material-retirement evidence and indexes. Quiesce retirement writers
-- and preserve compatible readers before binary rollback; never recreate parts.
SELECT 1;
