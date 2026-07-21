DELETE FROM system_configs
WHERE config_key IN (
  'task_email_enabled',
  'smtp_host',
  'smtp_port',
  'smtp_username',
  'smtp_password',
  'smtp_from',
  'smtp_tls_mode'
);
