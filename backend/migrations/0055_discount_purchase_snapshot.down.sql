-- Keep the wider column on binary rollback. Shrinking immutable purchase facts
-- to VARCHAR(2048) could destroy discount terms and is never automatic.
SELECT 1;
