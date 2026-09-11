-- +goose Up
ALTER TABLE users ADD COLUMN username TEXT;
UPDATE users SET username = 'legacy.' || id;
ALTER TABLE users ALTER COLUMN username SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_username_key UNIQUE(username);
ALTER TABLE users ADD CONSTRAINT users_username_nonempty CHECK (btrim(username) <> '');
ALTER TABLE users ADD COLUMN activated_at TIMESTAMPTZ;
-- Existing password-bearing accounts remain activated, including disabled accounts.
UPDATE users SET activated_at = created_at;
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
ALTER TABLE users ALTER COLUMN login_email DROP NOT NULL;
ALTER TABLE users DROP CONSTRAINT users_login_email_key;

ALTER TABLE memberships ADD COLUMN requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
UPDATE memberships SET requested_at = created_at;
ALTER TABLE memberships ADD COLUMN approved_at TIMESTAMPTZ;
ALTER TABLE memberships ADD COLUMN approved_by_user_id INTEGER REFERENCES users(id);
ALTER TABLE memberships ADD COLUMN admin_note TEXT;

CREATE TABLE membership_consent_requirements (
    membership_id INTEGER NOT NULL REFERENCES memberships(id),
    consent_definition_id INTEGER NOT NULL REFERENCES consent_definitions(id),
    presented_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (membership_id, consent_definition_id)
);
-- Historical presentation cannot be reconstructed: retain only evidenced definitions.
INSERT INTO membership_consent_requirements
SELECT membership_id, consent_definition_id, min(recorded_at)
FROM membership_consents GROUP BY membership_id, consent_definition_id;
CREATE TRIGGER membership_consent_requirements_immutable
BEFORE UPDATE OR DELETE ON membership_consent_requirements
FOR EACH ROW EXECUTE FUNCTION protect_membership_consent();

CREATE TABLE user_activation_codes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    code_hash BYTEA NOT NULL CHECK (octet_length(code_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    invalidated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (expires_at > created_at)
);
CREATE UNIQUE INDEX user_activation_codes_one_current ON user_activation_codes(user_id)
WHERE used_at IS NULL AND invalidated_at IS NULL;

-- +goose Down
-- Refuse lossy rollback when new accounts cannot satisfy the legacy constraints.
ALTER TABLE users ALTER COLUMN login_email SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_login_email_key UNIQUE(login_email);
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;
DROP TABLE user_activation_codes;
DROP TABLE membership_consent_requirements;
ALTER TABLE memberships DROP COLUMN admin_note, DROP COLUMN approved_by_user_id,
    DROP COLUMN approved_at, DROP COLUMN requested_at;
ALTER TABLE users DROP COLUMN activated_at, DROP COLUMN username;
