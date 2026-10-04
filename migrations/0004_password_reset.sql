-- +goose Up
CREATE TABLE user_password_reset_requests (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id INTEGER NOT NULL REFERENCES users(id),
 requested_by_user_id INTEGER REFERENCES users(id),
 token_hash BLOB NOT NULL UNIQUE CHECK (typeof(token_hash)='blob' AND length(token_hash)=32),
 created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')),
 expires_at DATETIME NOT NULL,
 used_at DATETIME,
 invalidated_at DATETIME,
 CHECK (expires_at>created_at),
 CHECK (used_at IS NULL OR invalidated_at IS NULL)
);
CREATE UNIQUE INDEX password_reset_one_current ON user_password_reset_requests(user_id)
 WHERE used_at IS NULL AND invalidated_at IS NULL;
CREATE INDEX password_reset_user_created ON user_password_reset_requests(user_id,created_at);
CREATE INDEX password_reset_expiration ON user_password_reset_requests(expires_at)
 WHERE used_at IS NULL AND invalidated_at IS NULL;

-- +goose Down
-- Preserve security history rather than silently dropping requests on rollback.
CREATE TABLE password_reset_rollback_guard (n INTEGER CHECK(n=0));
INSERT INTO password_reset_rollback_guard SELECT count(*) FROM user_password_reset_requests;
DROP TABLE password_reset_rollback_guard;
DROP TABLE user_password_reset_requests;
