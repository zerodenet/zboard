CREATE TABLE node_kernel_states (
  node_id BIGINT UNSIGNED NOT NULL,
  status VARCHAR(24) NOT NULL DEFAULT 'unknown',
  phase VARCHAR(32) NOT NULL DEFAULT 'idle',
  recommended_action VARCHAR(24) NOT NULL DEFAULT 'detect',
  platform_os VARCHAR(64) NOT NULL DEFAULT '',
  architecture VARCHAR(32) NOT NULL DEFAULT '',
  libc VARCHAR(64) NOT NULL DEFAULT '',
  desired_version VARCHAR(64) NOT NULL DEFAULT '',
  installed_version VARCHAR(64) NOT NULL DEFAULT '',
  desired_sha256 CHAR(64) NOT NULL DEFAULT '',
  installed_sha256 CHAR(64) NOT NULL DEFAULT '',
  desired_config_sha256 CHAR(64) NOT NULL DEFAULT '',
  applied_config_sha256 CHAR(64) NOT NULL DEFAULT '',
  service_status VARCHAR(24) NOT NULL DEFAULT 'unknown',
  control_status VARCHAR(24) NOT NULL DEFAULT 'unknown',
  last_error TEXT NOT NULL,
  active_operation_id BIGINT UNSIGNED NULL,
  last_detected_at DATETIME(3) NULL,
  last_healthy_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (node_id),
  KEY idx_node_kernel_states_active_operation (active_operation_id),
  CONSTRAINT fk_node_kernel_states_node FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
);

CREATE TABLE node_operations (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  node_id BIGINT UNSIGNED NOT NULL,
  operation_type VARCHAR(24) NOT NULL,
  status VARCHAR(24) NOT NULL DEFAULT 'running',
  phase VARCHAR(32) NOT NULL DEFAULT 'queued',
  requested_by BIGINT UNSIGNED NOT NULL,
  desired_version VARCHAR(64) NOT NULL DEFAULT '',
  desired_sha256 CHAR(64) NOT NULL DEFAULT '',
  artifact_url VARCHAR(512) NOT NULL DEFAULT '',
  result_summary TEXT NOT NULL,
  error TEXT NOT NULL,
  started_at DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_node_operations_node_created (node_id, created_at),
  KEY idx_node_operations_status (status),
  CONSTRAINT fk_node_operations_node FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE,
  CONSTRAINT fk_node_operations_requester FOREIGN KEY (requested_by) REFERENCES users(id)
);

INSERT INTO node_kernel_states (node_id, last_error)
SELECT id, '' FROM nodes;
