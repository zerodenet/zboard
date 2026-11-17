-- Stop settlement, drain pending daily projections with the new worker, then
-- verify the queue is empty before running an older writer. Preserve the table:
-- dropping reliable pending intents could silently lose already-settled usage.
SELECT 1;
