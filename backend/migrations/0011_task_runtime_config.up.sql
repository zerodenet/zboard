INSERT IGNORE INTO system_configs
  (config_key, name, value, value_type, description, is_public, is_secret)
VALUES
  ('task_email_enabled', '邮件任务开关', 'false', 'bool', '是否允许任务执行器发送邮件', 0, 0),
  ('smtp_host', 'SMTP 主机', '', 'string', '邮件服务器主机名', 0, 0),
  ('smtp_port', 'SMTP 端口', '587', 'int', '邮件服务器端口', 0, 0),
  ('smtp_username', 'SMTP 用户名', '', 'string', 'SMTP 登录用户名', 0, 0),
  ('smtp_password', 'SMTP 密码', '', 'string', 'SMTP 登录密码，使用凭证密钥加密保存', 0, 1),
  ('smtp_from', '发件地址', '', 'string', '邮件任务统一使用的发件邮箱', 0, 0),
  ('smtp_tls_mode', 'SMTP TLS 模式', 'starttls', 'string', '支持 starttls 或 implicit', 0, 0);
