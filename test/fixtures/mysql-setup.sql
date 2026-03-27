CREATE TABLE IF NOT EXISTS users (
  id INT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(255) NOT NULL,
  email VARCHAR(255) UNIQUE,
  bio TEXT
);
CREATE TABLE IF NOT EXISTS posts (
  id INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT,
  title VARCHAR(255) NOT NULL,
  body TEXT,
  published TINYINT DEFAULT 0,
  FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX idx_posts_user_id ON posts(user_id);
INSERT IGNORE INTO users (id, name, email, bio) VALUES (1, 'Alice', 'alice@test.com', 'A developer');
INSERT IGNORE INTO users (id, name, email) VALUES (2, 'Bob', 'bob@test.com');
INSERT IGNORE INTO posts (id, user_id, title, body, published) VALUES (1, 1, 'Hello', 'Content', 1);
