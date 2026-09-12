-- +goose Up
CREATE TABLE registration_verification_outbox (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 submission_id INTEGER NOT NULL REFERENCES registration_submissions(id),
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','sent','dead','cancelled')),
 attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count BETWEEN 0 AND 3),
 available_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 lease_until TIMESTAMPTZ,
 lease_version BIGINT NOT NULL DEFAULT 0 CHECK(lease_version>=0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 sent_at TIMESTAMPTZ,
 finished_at TIMESTAMPTZ,
 last_error_code TEXT CHECK(last_error_code IN ('smtp_failed','ineligible','attempts_exhausted','mailer_disabled','submission_closed')),
 recipient_hash BYTEA NOT NULL CHECK(octet_length(recipient_hash)=32),
 CHECK((status='processing') = (lease_until IS NOT NULL)),
 CHECK((status IN ('sent','dead','cancelled')) = (finished_at IS NOT NULL)),
 CHECK((status='sent') = (sent_at IS NOT NULL))
);
CREATE UNIQUE INDEX registration_outbox_one_active ON registration_verification_outbox(submission_id)
 WHERE status IN ('pending','processing');
CREATE INDEX registration_outbox_pending ON registration_verification_outbox(available_at,id) WHERE status='pending';
CREATE INDEX registration_outbox_lease ON registration_verification_outbox(lease_until,id) WHERE status='processing';
CREATE INDEX registration_outbox_recipient ON registration_verification_outbox(recipient_hash,created_at);
-- Retain terminal delivery history, including failures and cancellations.
-- +goose StatementBegin
CREATE FUNCTION protect_registration_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'outbox history is retained'; END IF;
 IF OLD.status IN ('sent','dead','cancelled') THEN RAISE EXCEPTION 'outbox history is final'; END IF;
 IF (NEW.id,NEW.submission_id,NEW.recipient_hash,NEW.created_at) IS DISTINCT FROM
    (OLD.id,OLD.submission_id,OLD.recipient_hash,OLD.created_at) THEN
  RAISE EXCEPTION 'outbox intent is immutable';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER registration_outbox_audit BEFORE UPDATE OR DELETE ON registration_verification_outbox
 FOR EACH ROW EXECUTE FUNCTION protect_registration_outbox();

-- Administrative and email resolution cancel outstanding intents atomically.
-- +goose StatementBegin
CREATE FUNCTION cancel_registration_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status IN ('resolved','cancelled') AND OLD.status IS DISTINCT FROM NEW.status THEN
  UPDATE registration_verification_outbox SET status='cancelled',lease_until=NULL,
   finished_at=clock_timestamp(),updated_at=clock_timestamp(),last_error_code='submission_closed'
   WHERE submission_id=NEW.id AND status IN ('pending','processing');
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER registration_outbox_closed AFTER UPDATE ON registration_submissions
 FOR EACH ROW EXECUTE FUNCTION cancel_registration_outbox();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM registration_verification_outbox) THEN
  RAISE EXCEPTION 'cannot discard registration outbox history';
 END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER registration_outbox_closed ON registration_submissions;
DROP FUNCTION cancel_registration_outbox();
DROP TABLE registration_verification_outbox;
DROP FUNCTION protect_registration_outbox();
