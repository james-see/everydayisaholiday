package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir data: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL COLLATE NOCASE UNIQUE,
  password_hash TEXT NOT NULL,
  email_verified_at TEXT,
  verify_token_hash TEXT,
  verify_token_expires_at TEXT,
  reset_token_hash TEXT,
  reset_token_expires_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS email_prefs (
  user_id INTEGER PRIMARY KEY,
  enabled INTEGER NOT NULL DEFAULT 0,
  timezone TEXT NOT NULL DEFAULT 'UTC',
  categories TEXT NOT NULL DEFAULT '[]',
  unsub_token TEXT NOT NULL UNIQUE,
  last_sent_local_date TEXT,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_email_prefs_enabled ON email_prefs(enabled);
CREATE INDEX IF NOT EXISTS idx_email_prefs_unsub_token ON email_prefs(unsub_token);

CREATE TABLE IF NOT EXISTS api_keys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  rate_tier TEXT NOT NULL DEFAULT 'free',
  created_at TEXT NOT NULL,
  revoked_at TEXT,
  last_used_at TEXT,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);

CREATE TABLE IF NOT EXISTS oauth_clients (
  client_id TEXT PRIMARY KEY,
  client_secret_hash TEXT,
  client_name TEXT NOT NULL DEFAULT '',
  redirect_uris TEXT NOT NULL,
  grant_types TEXT NOT NULL DEFAULT '["authorization_code"]',
  token_endpoint_auth_method TEXT NOT NULL DEFAULT 'none',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS oauth_auth_codes (
  code_hash TEXT PRIMARY KEY,
  client_id TEXT NOT NULL,
  user_id INTEGER NOT NULL,
  redirect_uri TEXT NOT NULL,
  code_challenge TEXT NOT NULL,
  code_challenge_method TEXT NOT NULL DEFAULT 'S256',
  scope TEXT NOT NULL DEFAULT '',
  resource TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS oauth_access_tokens (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  client_id TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT '',
  resource TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  revoked_at TEXT,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_oauth_access_tokens_user ON oauth_access_tokens(user_id);

CREATE TABLE IF NOT EXISTS rate_limits (
  bucket_key TEXT NOT NULL,
  window_start INTEGER NOT NULL,
  count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket_key, window_start)
);
`)
	return err
}
