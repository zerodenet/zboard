-- Preserve complete, bounded provider agreement evidence alongside the locked
-- purchase price. Existing snapshot bytes are retained; no history is inferred.
ALTER TABLE orders MODIFY COLUMN purchase_snapshot TEXT NOT NULL DEFAULT ('');
