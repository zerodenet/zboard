-- Identity evidence must survive binary rollback. Do not erase tags/receipt.
-- Older binaries/compactors may lose optional envelope metadata; fence them
-- before eventually relying on labels for observation admission.
SELECT 1;
