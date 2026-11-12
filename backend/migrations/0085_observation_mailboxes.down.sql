-- Preserve pending projections; do not drop runtime state during rollback.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='observation mailbox rollback requires compatibility review';
