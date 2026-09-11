-- +goose Up
CREATE TABLE registration_submissions (
 id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 status TEXT NOT NULL DEFAULT 'received' CHECK (status IN ('received','awaiting_identity_review','resolved','cancelled')),
 first_name TEXT NOT NULL CHECK (btrim(first_name)<>''),
 last_name TEXT NOT NULL CHECK (btrim(last_name)<>''),
 birth_date DATE,
 email TEXT,
 phone_number TEXT,
 address TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 resolved_person_id INTEGER REFERENCES persons(id),
 resolution_type TEXT CHECK (resolution_type IN ('existing_person','new_person')),
 resolved_at TIMESTAMPTZ,
 resolved_by_user_id INTEGER REFERENCES users(id),
 CONSTRAINT registration_resolution_consistent CHECK (
  (status='resolved' AND resolved_person_id IS NOT NULL AND resolution_type IS NOT NULL AND resolved_at IS NOT NULL)
  OR
  (status<>'resolved' AND resolved_person_id IS NULL AND resolution_type IS NULL AND resolved_at IS NULL AND resolved_by_user_id IS NULL)
 )
);
CREATE INDEX registration_submissions_queue_idx ON registration_submissions(status,created_at,id);

CREATE TABLE registration_submission_candidates (
 submission_id INTEGER NOT NULL REFERENCES registration_submissions(id),
 person_id INTEGER NOT NULL REFERENCES persons(id),
 confidence TEXT NOT NULL CHECK (confidence IN ('strong','possible','weak')),
 matched_name BOOLEAN NOT NULL,
 matched_birth_date BOOLEAN NOT NULL,
 matched_email BOOLEAN NOT NULL,
 matched_phone BOOLEAN NOT NULL,
 detected_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (submission_id,person_id)
);
CREATE INDEX registration_submission_candidates_person_idx ON registration_submission_candidates(person_id);

-- Retain declared evidence and make final resolutions immutable, including direct SQL.
-- +goose StatementBegin
CREATE FUNCTION protect_registration_submission() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION 'registration submissions are retained for audit' USING ERRCODE='23514';
 END IF;
 IF OLD.status IN ('resolved','cancelled') THEN
  RAISE EXCEPTION 'registration resolution is final' USING ERRCODE='23514';
 END IF;
 IF (NEW.id,NEW.first_name,NEW.last_name,NEW.birth_date,NEW.email,NEW.phone_number,NEW.address,NEW.created_at)
 IS DISTINCT FROM (OLD.id,OLD.first_name,OLD.last_name,OLD.birth_date,OLD.email,OLD.phone_number,OLD.address,OLD.created_at) THEN
  RAISE EXCEPTION 'declared registration identity is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER registration_submissions_audit BEFORE UPDATE OR DELETE ON registration_submissions
FOR EACH ROW EXECUTE FUNCTION protect_registration_submission();

-- +goose StatementBegin
CREATE FUNCTION protect_registration_candidate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'registration candidate evidence is immutable' USING ERRCODE='23514';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER registration_candidates_audit BEFORE UPDATE OR DELETE ON registration_submission_candidates
FOR EACH ROW EXECUTE FUNCTION protect_registration_candidate();

-- +goose Down
-- Never discard staging/audit data during rollback. An empty installation can roll back.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM registration_submissions) THEN
  RAISE EXCEPTION 'cannot roll back registration audit data';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE registration_submission_candidates;
DROP TABLE registration_submissions;
DROP FUNCTION protect_registration_candidate();
DROP FUNCTION protect_registration_submission();
