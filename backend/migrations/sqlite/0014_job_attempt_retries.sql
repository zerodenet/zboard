ALTER TABLE job_runs ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE job_runs ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 1;
ALTER TABLE job_runs ADD COLUMN retry_backoff_ms INTEGER NOT NULL DEFAULT 0;
CREATE TABLE job_attempts_v2 (
 token TEXT PRIMARY KEY, run_id TEXT NOT NULL, attempt_number INTEGER NOT NULL,
 worker TEXT NOT NULL, state TEXT NOT NULL, started_at DATETIME NOT NULL,
 expires_at DATETIME NOT NULL, finished_at DATETIME,
 UNIQUE(run_id,attempt_number)
);
INSERT INTO job_attempts_v2 (token,run_id,attempt_number,worker,state,started_at,expires_at,finished_at)
 SELECT token,run_id,1,worker,state,started_at,expires_at,finished_at FROM job_attempts;
DROP TABLE job_attempts;
ALTER TABLE job_attempts_v2 RENAME TO job_attempts;
ALTER TABLE job_schedules ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 1;
ALTER TABLE job_schedules ADD COLUMN retry_backoff_ms INTEGER NOT NULL DEFAULT 0;
