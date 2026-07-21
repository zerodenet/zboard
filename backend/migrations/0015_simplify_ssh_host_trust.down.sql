ALTER TABLE nodes
  ADD COLUMN ssh_host_key_policy VARCHAR(24) NOT NULL DEFAULT 'trust_on_first_use' AFTER ssh_host_key_fingerprint;

UPDATE nodes
SET ssh_host_key_policy = CASE
  WHEN ssh_host_key_fingerprint IS NULL OR ssh_host_key_fingerprint = '' THEN 'trust_on_first_use'
  ELSE 'strict'
END;
