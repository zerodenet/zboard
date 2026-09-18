CREATE TABLE job_execution_budget (
 id bigint unsigned NOT NULL PRIMARY KEY, capacity int NOT NULL, revision bigint unsigned NOT NULL
) ENGINE=InnoDB;
INSERT INTO job_execution_budget (id,capacity,revision) VALUES (1,4,0);
CREATE TABLE job_execution_groups (id varchar(160) NOT NULL PRIMARY KEY, capacity int NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
INSERT INTO job_execution_groups (id,capacity) VALUES ('external',3);
CREATE TABLE job_runs (
 execution_group varchar(160) NOT NULL DEFAULT '', KEY job_group_state(execution_group,state),
 id varchar(36) NOT NULL PRIMARY KEY, owner varchar(200) NOT NULL, `key` varchar(160) NOT NULL,
 handler varchar(320) NOT NULL, resource varchar(200) NOT NULL, payload text NOT NULL,
 fingerprint varchar(64) NOT NULL, state varchar(24) NOT NULL, not_before datetime(3) NOT NULL,
 created_at datetime(3) NOT NULL, finished_at datetime(3) DEFAULT NULL, token varchar(36) NOT NULL,
 worker varchar(160) NOT NULL, expires_at datetime(3) DEFAULT NULL, schedule_id varchar(320) NOT NULL DEFAULT '',
 UNIQUE KEY job_intent(owner,`key`), KEY job_ready(state,not_before), KEY job_resource(resource,state), KEY job_schedule(schedule_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE job_attempts (
 token varchar(36) NOT NULL PRIMARY KEY, run_id varchar(36) NOT NULL, worker varchar(160) NOT NULL,
 state varchar(24) NOT NULL, started_at datetime(3) NOT NULL, expires_at datetime(3) NOT NULL,
 finished_at datetime(3) DEFAULT NULL, UNIQUE KEY job_attempt_run(run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE job_schedules (
 id varchar(320) NOT NULL PRIMARY KEY, owner varchar(200) NOT NULL, name varchar(160) NOT NULL,
 handler varchar(320) NOT NULL, resource varchar(200) NOT NULL, revision varchar(160) NOT NULL,
 interval_ms bigint NOT NULL, timeout_ms bigint NOT NULL, next_at datetime(3) NOT NULL,
 sequence bigint unsigned NOT NULL, run_id varchar(36) NOT NULL, runs bigint unsigned NOT NULL,
 failures bigint unsigned NOT NULL, state varchar(24) NOT NULL DEFAULT '', last_state varchar(24) NOT NULL,
 last_started_at datetime(3) DEFAULT NULL, last_finished_at datetime(3) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
