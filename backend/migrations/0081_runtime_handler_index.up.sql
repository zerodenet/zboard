ALTER TABLE job_runs ADD INDEX job_runtime_handler (owner, handler, finished_at), ALGORITHM=INPLACE, LOCK=NONE;
