ALTER TABLE job_schedules
 DROP COLUMN retry_backoff_ms,
 DROP COLUMN max_attempts;
DELETE FROM job_attempts WHERE attempt_number > 1;
ALTER TABLE job_attempts
 DROP INDEX job_attempt_run_number,
 DROP COLUMN attempt_number,
 ADD UNIQUE KEY job_attempt_run(run_id);
ALTER TABLE job_runs
 DROP COLUMN retry_backoff_ms,
 DROP COLUMN max_attempts,
 DROP COLUMN attempt_count;
