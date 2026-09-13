-- +goose Up
ALTER TABLE persons ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp();
CREATE TABLE user_email_change_requests (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id),
 person_id INTEGER NOT NULL REFERENCES persons(id),
 new_email TEXT NOT NULL,
 new_email_normalized TEXT NOT NULL,
 new_email_hash BYTEA NOT NULL CHECK (octet_length(new_email_hash)=32),
 code_hash BYTEA NOT NULL CHECK (octet_length(code_hash)=32),
 expires_at TIMESTAMPTZ NOT NULL,
 used_at TIMESTAMPTZ,
 invalidated_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE UNIQUE INDEX user_email_change_one_active ON user_email_change_requests(user_id)
 WHERE used_at IS NULL AND invalidated_at IS NULL;
CREATE INDEX user_email_change_budget ON user_email_change_requests(user_id,created_at);
CREATE TABLE account_security_events (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id),
 person_id INTEGER NOT NULL REFERENCES persons(id),
 event TEXT NOT NULL CHECK (event IN ('email_change_requested','email_changed','password_changed','profile_contact_updated')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
-- +goose Down
-- Do not silently discard security history.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM account_security_events) OR EXISTS(SELECT 1 FROM user_email_change_requests) THEN
 RAISE EXCEPTION 'cannot discard account security history'; END IF;
END $$;
-- +goose StatementEnd
DROP TABLE account_security_events;
DROP TABLE user_email_change_requests;
ALTER TABLE persons DROP COLUMN updated_at;
