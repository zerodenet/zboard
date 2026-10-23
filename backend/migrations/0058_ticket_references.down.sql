-- Preserve historical support context. Nullable columns can be ignored by
-- compatible readers, but older strict migration inventories may refuse 0058.
-- Use a compatible fix; never erase references or receipts for binary rollback.
SELECT 1;
