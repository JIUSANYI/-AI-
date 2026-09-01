CREATE TABLE IF NOT EXISTS link_cards (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  answer_id BIGINT UNSIGNED NOT NULL,
  url VARCHAR(2048) NOT NULL,
  title VARCHAR(512) NULL,
  description TEXT NULL,
  image_url VARCHAR(2048) NULL,
  media_type VARCHAR(20) NOT NULL,
  position SMALLINT UNSIGNED NOT NULL,
  site_name VARCHAR(255) NULL,
  INDEX idx_link_cards_answer_position (answer_id, position),
  CONSTRAINT fk_link_cards_answer FOREIGN KEY (answer_id) REFERENCES answers(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
