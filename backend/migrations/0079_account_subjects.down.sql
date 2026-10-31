-- Account deletion is irreversible. Historical subjects cannot be mapped back
-- to deleted login accounts. Rolling back this migration is unsupported.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Account subject migration cannot be rolled back after account deletion';
