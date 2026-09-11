-- +goose Up
ALTER TABLE registration_submissions DROP CONSTRAINT registration_submissions_status_check;
ALTER TABLE registration_submissions ADD CONSTRAINT registration_submissions_status_check CHECK
(status IN ('received','awaiting_identity_review','awaiting_email_verification','resolved','cancelled'));
CREATE TABLE registration_email_verifications (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 submission_id INTEGER NOT NULL,
 person_id INTEGER NOT NULL,
 public_reference TEXT NOT NULL UNIQUE CHECK (length(public_reference)=64),
 code_hash BYTEA NOT NULL CHECK (octet_length(code_hash)=32),
 recipient_hash BYTEA NOT NULL CHECK (octet_length(recipient_hash)=32),
 expires_at TIMESTAMPTZ NOT NULL,
 used_at TIMESTAMPTZ,
 invalidated_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(submission_id,person_id) REFERENCES registration_submission_candidates(submission_id,person_id),
 CHECK (used_at IS NULL OR invalidated_at IS NULL),
 CHECK (expires_at>created_at)
);
CREATE UNIQUE INDEX registration_email_one_open ON registration_email_verifications(submission_id)
WHERE used_at IS NULL AND invalidated_at IS NULL;
CREATE INDEX registration_email_expiration ON registration_email_verifications(expires_at)
WHERE used_at IS NULL AND invalidated_at IS NULL;
-- +goose StatementBegin
CREATE FUNCTION protect_registration_email_proof() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'verification evidence is retained'; END IF;
 IF OLD.used_at IS NOT NULL OR OLD.invalidated_at IS NOT NULL THEN
  RAISE EXCEPTION 'verification evidence is final';
 END IF;
 IF (NEW.id,NEW.submission_id,NEW.person_id,NEW.public_reference,NEW.code_hash,NEW.recipient_hash,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM (OLD.id,OLD.submission_id,OLD.person_id,OLD.public_reference,OLD.code_hash,OLD.recipient_hash,OLD.created_at,OLD.expires_at) THEN
  RAISE EXCEPTION 'verification evidence is immutable';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER registration_email_audit BEFORE UPDATE OR DELETE ON registration_email_verifications
FOR EACH ROW EXECUTE FUNCTION protect_registration_email_proof();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM registration_email_verifications)
 OR EXISTS(SELECT 1 FROM registration_submissions WHERE status='awaiting_email_verification') THEN
  RAISE EXCEPTION 'cannot discard registration verification history';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE registration_email_verifications;
DROP FUNCTION protect_registration_email_proof();
ALTER TABLE registration_submissions DROP CONSTRAINT registration_submissions_status_check;
ALTER TABLE registration_submissions ADD CONSTRAINT registration_submissions_status_check CHECK
(status IN ('received','awaiting_identity_review','resolved','cancelled'));
