CREATE INDEX idx_job_retention ON job_runs(state,finished_at,id);
CREATE INDEX idx_task_retention ON tasks(finished_at,id);
CREATE INDEX idx_node_operation_retention ON node_operations(finished_at,id);
CREATE INDEX idx_deployment_retention ON protocol_deployments(finished_at,id);
CREATE INDEX idx_certificate_operation_retention ON certificate_operations(finished_at,id);
CREATE INDEX idx_provider_operation_retention ON provider_operations(finished_at,id);
