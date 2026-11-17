-- Retain pending/held source evidence and independent positions. Downgrade
-- cannot safely reconstruct discarded sources; owned binary rollback retains
-- these tables and receipt. Older binaries must not consume this handoff.
SELECT 1;
