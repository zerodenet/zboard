ALTER TABLE nodes
  ADD COLUMN ssh_privilege_mode VARCHAR(16) NOT NULL DEFAULT 'none' AFTER ssh_private_key_passphrase,
  ADD COLUMN ssh_privilege_password TEXT NULL AFTER ssh_privilege_mode;
