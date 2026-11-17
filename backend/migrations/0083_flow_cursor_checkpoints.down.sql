-- Accounting checkpoints may already be the only surviving cumulative floor.
-- No destructive automatic downgrade is safe after compaction.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'restore completed flow authority before downgrade';
