-- Earlier binaries require privileged triggers and cannot safely maintain the
-- projection after this change. Restore the pre-upgrade database and binary.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '0025 requires a database snapshot restore for rollback; do not downgrade only the binary';
