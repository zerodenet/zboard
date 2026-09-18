ALTER TABLE job_execution_budget
 ADD COLUMN dispatch_virtual_time bigint unsigned NOT NULL DEFAULT 0 AFTER revision;
CREATE TABLE job_dispatch_lanes (
 lane varchar(200) NOT NULL PRIMARY KEY,
 virtual_finish bigint unsigned NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
ALTER TABLE job_runs
 ADD COLUMN dispatch_lane varchar(200) NOT NULL DEFAULT 'core' AFTER execution_group,
 ADD COLUMN planned_at datetime(3) DEFAULT NULL AFTER schedule_id,
 ADD KEY job_dispatch_ready(dispatch_lane,state,not_before),
 ADD UNIQUE KEY job_schedule_planned(schedule_id,planned_at);
UPDATE job_runs
 SET dispatch_lane = CASE
  WHEN owner LIKE 'plugin:%' THEN owner
  WHEN execution_group <> '' THEN CONCAT('group:', execution_group)
  ELSE 'core'
 END;
ALTER TABLE job_schedules
 ADD COLUMN execution_group varchar(160) NOT NULL DEFAULT '' AFTER resource,
 ADD COLUMN timezone varchar(64) NOT NULL DEFAULT 'UTC' AFTER retry_backoff_ms,
 ADD COLUMN misfire_policy varchar(24) NOT NULL DEFAULT 'fire_once' AFTER timezone,
 ADD COLUMN dispatch_lane varchar(200) NOT NULL DEFAULT 'core' AFTER misfire_policy,
 ADD COLUMN anchor_at datetime(3) DEFAULT NULL AFTER dispatch_lane,
 ADD COLUMN missed_runs bigint unsigned NOT NULL DEFAULT 0 AFTER failures;
UPDATE job_schedules
 SET dispatch_lane = CASE WHEN owner LIKE 'plugin:%' THEN owner ELSE 'core' END;
