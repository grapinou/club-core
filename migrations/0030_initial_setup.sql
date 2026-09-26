-- +goose Up
-- Existing installations with accounts are closed to web setup on upgrade.
-- A fresh database has an explicit, uninitialized row but no usable secret.
CREATE TABLE installation_setup (
 id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
 initialized_at TIMESTAMPTZ,
 first_admin_user_id INTEGER REFERENCES users(id),
 secret_hash BYTEA CHECK (secret_hash IS NULL OR octet_length(secret_hash)=32),
 secret_issued_at TIMESTAMPTZ,
 secret_generation INTEGER NOT NULL DEFAULT 0 CHECK (secret_generation>=0),
 CHECK (initialized_at IS NULL OR secret_hash IS NULL),
 CHECK ((secret_hash IS NULL) = (secret_issued_at IS NULL))
);
INSERT INTO installation_setup(id,initialized_at)
SELECT TRUE,CASE WHEN EXISTS(SELECT 1 FROM users) THEN clock_timestamp() ELSE NULL END;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM installation_setup WHERE secret_generation>0 OR first_admin_user_id IS NOT NULL) THEN
  RAISE EXCEPTION 'cannot discard installation setup history';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE installation_setup;
