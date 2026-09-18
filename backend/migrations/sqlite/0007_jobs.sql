CREATE TABLE IF NOT EXISTS job_execution_budget (id INTEGER PRIMARY KEY, capacity INTEGER NOT NULL, revision INTEGER NOT NULL);
INSERT OR IGNORE INTO job_execution_budget (id,capacity,revision) VALUES (1,4,0);
CREATE TABLE IF NOT EXISTS job_execution_groups (id TEXT PRIMARY KEY, capacity INTEGER NOT NULL);
INSERT OR IGNORE INTO job_execution_groups (id,capacity) VALUES ('external',3);
CREATE TABLE IF NOT EXISTS job_runs (
 execution_group TEXT NOT NULL DEFAULT '',
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, `key` TEXT NOT NULL, handler TEXT NOT NULL,
 resource TEXT NOT NULL, payload TEXT NOT NULL, fingerprint TEXT NOT NULL, state TEXT NOT NULL,
 not_before DATETIME NOT NULL, created_at DATETIME NOT NULL, finished_at DATETIME,
 token TEXT NOT NULL, worker TEXT NOT NULL, expires_at DATETIME, schedule_id TEXT NOT NULL DEFAULT '',
 UNIQUE(owner,`key`)
);
CREATE INDEX IF NOT EXISTS job_ready ON job_runs(state,not_before);
CREATE INDEX IF NOT EXISTS job_resource ON job_runs(resource,state);
CREATE INDEX IF NOT EXISTS job_schedule ON job_runs(schedule_id);
CREATE TABLE IF NOT EXISTS job_attempts (
 token TEXT PRIMARY KEY, run_id TEXT NOT NULL UNIQUE, worker TEXT NOT NULL, state TEXT NOT NULL,
 started_at DATETIME NOT NULL, expires_at DATETIME NOT NULL, finished_at DATETIME
);
CREATE TABLE IF NOT EXISTS job_schedules (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, name TEXT NOT NULL, handler TEXT NOT NULL,
 resource TEXT NOT NULL, revision TEXT NOT NULL, interval_ms INTEGER NOT NULL, timeout_ms INTEGER NOT NULL,
 next_at DATETIME NOT NULL, sequence INTEGER NOT NULL, run_id TEXT NOT NULL,
 runs INTEGER NOT NULL, failures INTEGER NOT NULL, state TEXT NOT NULL DEFAULT '', last_state TEXT NOT NULL,
 last_started_at DATETIME, last_finished_at DATETIME
);

CREATE INDEX IF NOT EXISTS job_group_state ON job_runs(execution_group,state);
