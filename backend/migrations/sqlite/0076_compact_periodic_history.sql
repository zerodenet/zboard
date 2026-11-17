ALTER TABLE job_schedules ADD COLUMN compact_history numeric NOT NULL DEFAULT false;
ALTER TABLE job_schedules ADD COLUMN compact_run_id text NOT NULL DEFAULT '';
