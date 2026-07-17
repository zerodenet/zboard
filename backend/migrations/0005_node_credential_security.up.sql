ALTER TABLE nodes
  MODIFY COLUMN ssh_pwd TEXT NULL,
  ADD COLUMN IF NOT EXISTS ssh_host_key_fingerprint VARCHAR(128) NULL AFTER ssh_pwd;
