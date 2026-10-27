-- Identity and access: relational, small, strongly consistent, joined on every request.
CREATE TABLE IF NOT EXISTS teams (
  id   INT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(64) NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS users (
  id      INT AUTO_INCREMENT PRIMARY KEY,
  name    VARCHAR(64) NOT NULL UNIQUE,
  role    ENUM('viewer', 'operator', 'admin') NOT NULL,
  team_id INT NOT NULL,
  FOREIGN KEY (team_id) REFERENCES teams(id)
);

-- Only the SHA-256 hex digest of a key is stored, never the key itself.
CREATE TABLE IF NOT EXISTS api_keys (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  user_id    INT NOT NULL,
  key_hash   CHAR(64) NOT NULL UNIQUE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  revoked_at TIMESTAMP NULL,
  FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS collection_access (
  team_id    INT NOT NULL,
  collection VARCHAR(64) NOT NULL,
  PRIMARY KEY (team_id, collection),
  FOREIGN KEY (team_id) REFERENCES teams(id)
);
