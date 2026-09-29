-- The previous flags cannot be reconstructed without a database snapshot.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Restore the pre-upgrade database snapshot to roll back fixed quota lifecycle';
