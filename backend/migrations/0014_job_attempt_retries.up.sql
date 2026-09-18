ALTER TABLE job_runs
 ADD COLUMN attempt_count int unsigned NOT NULL DEFAULT 0 AFTER timeout_ms,
 ADD COLUMN max_attempts int unsigned NOT NULL DEFAULT 1 AFTER attempt_count,
 ADD COLUMN retry_backoff_ms bigint NOT NULL DEFAULT 0 AFTER max_attempts;
ALTER TABLE job_attempts
 DROP INDEX job_attempt_run,
 ADD COLUMN attempt_number int unsigned NOT NULL DEFAULT 1 AFTER run_id,
 ADD UNIQUE KEY job_attempt_run_number(run_id,attempt_number);
ALTER TABLE job_schedules
 ADD COLUMN max_attempts int unsigned NOT NULL DEFAULT 1 AFTER timeout_ms,
 ADD COLUMN retry_backoff_ms bigint NOT NULL DEFAULT 0 AFTER max_attempts;
