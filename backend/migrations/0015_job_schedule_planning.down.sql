ALTER TABLE job_schedules
 DROP COLUMN missed_runs,
 DROP COLUMN anchor_at,
 DROP COLUMN dispatch_lane,
 DROP COLUMN misfire_policy,
 DROP COLUMN timezone,
 DROP COLUMN execution_group;
ALTER TABLE job_runs
 DROP INDEX job_schedule_planned,
 DROP INDEX job_dispatch_ready,
 DROP COLUMN planned_at,
 DROP COLUMN dispatch_lane;
DROP TABLE IF EXISTS job_dispatch_lanes;
ALTER TABLE job_execution_budget DROP COLUMN dispatch_virtual_time;
