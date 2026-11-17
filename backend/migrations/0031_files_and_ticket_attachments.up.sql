CREATE TABLE stored_files (
 id varchar(36) NOT NULL PRIMARY KEY,
 owner_id bigint unsigned NOT NULL,
 purpose varchar(16) NOT NULL,
 name varchar(255) NOT NULL,
 content_type varchar(80) NOT NULL,
 size bigint NOT NULL,
 created_at datetime(3) NOT NULL,
 deleted_at datetime(3) NULL,
 KEY idx_stored_files_owner_id (owner_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE ticket_attachments (
 id bigint unsigned NOT NULL AUTO_INCREMENT PRIMARY KEY,
 message_id bigint unsigned NOT NULL,
 file_id varchar(36) NULL,
 name varchar(255) NOT NULL,
 url varchar(2048) NOT NULL,
 KEY idx_ticket_attachments_message_id (message_id),
 UNIQUE KEY idx_ticket_attachments_file_id (file_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
