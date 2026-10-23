-- Financial evidence must survive binary rollback. Do not drop wallet tables
-- or replace nullable top-up references with dummy products.
SELECT 1;
