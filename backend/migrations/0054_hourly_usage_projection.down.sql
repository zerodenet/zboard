-- Stop settlement and drain all pending work with the current worker before
-- starting an old synchronous writer. Preserve evidence and all derived data.
-- A pre-0054 SQLite binary also needs its original hourly triggers restored
-- after draining by datastore.PrepareHourlyProjectionRollback with all writers
-- stopped; never run the restored triggers together with a post-0054 writer.
SELECT 1;
