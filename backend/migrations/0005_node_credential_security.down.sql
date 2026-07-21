-- Keep ssh_pwd as TEXT so encrypted credentials remain intact for forward recovery.
ALTER TABLE nodes
  DROP COLUMN ssh_host_key_fingerprint;
