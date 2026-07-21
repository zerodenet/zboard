CREATE TABLE tickets (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ticket_no VARCHAR(32) NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  subject VARCHAR(160) NOT NULL,
  category VARCHAR(32) NOT NULL,
  priority SMALLINT NOT NULL DEFAULT 1,
  status VARCHAR(24) NOT NULL DEFAULT 'open',
  last_message_at DATETIME(3) NOT NULL,
  resolved_at DATETIME(3) NULL,
  closed_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_tickets_ticket_no (ticket_no),
  INDEX idx_tickets_user_status (user_id, status),
  INDEX idx_tickets_queue (status, priority, last_message_at),
  INDEX idx_tickets_category (category),
  CONSTRAINT fk_tickets_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE ticket_messages (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ticket_id BIGINT UNSIGNED NOT NULL,
  author_id BIGINT UNSIGNED NULL,
  author_role VARCHAR(16) NOT NULL,
  message_type VARCHAR(16) NOT NULL,
  body TEXT NOT NULL,
  from_status VARCHAR(24) NOT NULL DEFAULT '',
  to_status VARCHAR(24) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  INDEX idx_ticket_messages_ticket_time (ticket_id, created_at, id),
  INDEX idx_ticket_messages_author (author_id),
  CONSTRAINT fk_ticket_messages_ticket FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE,
  CONSTRAINT fk_ticket_messages_author FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
