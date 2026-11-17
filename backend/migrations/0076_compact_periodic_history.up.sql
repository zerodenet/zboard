ALTER TABLE job_schedules ADD COLUMN compact_history boolean NOT NULL DEFAULT false;
ALTER TABLE job_schedules ADD COLUMN compact_run_id varchar(36) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '';
