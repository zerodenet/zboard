ALTER TABLE job_execution_budget ADD COLUMN dispatch_virtual_time INTEGER NOT NULL DEFAULT 0;
CREATE TABLE job_dispatch_lanes (
 lane TEXT PRIMARY KEY,
 virtual_finish INTEGER NOT NULL
);
ALTER TABLE job_runs ADD COLUMN dispatch_lane TEXT NOT NULL DEFAULT 'core';
ALTER TABLE job_runs ADD COLUMN planned_at DATETIME;
UPDATE job_runs
 SET dispatch_lane = CASE
  WHEN owner LIKE 'plugin:%' THEN owner
  WHEN execution_group <> '' THEN 'group:' || execution_group
  ELSE 'core'
 END;
CREATE INDEX job_dispatch_ready ON job_runs(dispatch_lane,state,not_before);
CREATE UNIQUE INDEX job_schedule_planned ON job_runs(schedule_id,planned_at);
ALTER TABLE job_schedules ADD COLUMN execution_group TEXT NOT NULL DEFAULT '';
ALTER TABLE job_schedules ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC';
ALTER TABLE job_schedules ADD COLUMN misfire_policy TEXT NOT NULL DEFAULT 'fire_once';
ALTER TABLE job_schedules ADD COLUMN dispatch_lane TEXT NOT NULL DEFAULT 'core';
ALTER TABLE job_schedules ADD COLUMN anchor_at DATETIME;
ALTER TABLE job_schedules ADD COLUMN missed_runs INTEGER NOT NULL DEFAULT 0;
UPDATE job_schedules
 SET dispatch_lane = CASE WHEN owner LIKE 'plugin:%' THEN owner ELSE 'core' END;
