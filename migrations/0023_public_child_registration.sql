-- +goose Up
ALTER TABLE registration_applications DROP CONSTRAINT registration_applications_last_error_code_check;
ALTER TABLE registration_applications ADD CONSTRAINT registration_applications_last_error_code_check CHECK(last_error_code IN ('membership_already_exists','choices_unavailable','member_not_adult','membership_unavailable','guardian_identity_review','child_identity_review','guardian_confirmation_required','member_not_minor','guardian_relation_invalid'));
CREATE TABLE guardian_identity_claims (
 id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 status TEXT NOT NULL DEFAULT 'received' CHECK(status IN ('received','awaiting_review','resolved','cancelled')),
 first_name TEXT NOT NULL CHECK(btrim(first_name)<>''),
 last_name TEXT NOT NULL CHECK(btrim(last_name)<>''),
 birth_date DATE, email TEXT NOT NULL CHECK(btrim(email)<>''), phone_number TEXT, address TEXT,
 resolved_person_id INTEGER REFERENCES persons(id),
 resolution_type TEXT CHECK(resolution_type IN ('existing_person','new_person')),
 resolved_at TIMESTAMPTZ, resolved_by_user_id INTEGER REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CHECK ((status='resolved' AND resolved_person_id IS NOT NULL AND resolution_type IS NOT NULL AND resolved_at IS NOT NULL) OR
 (status<>'resolved' AND resolved_person_id IS NULL AND resolution_type IS NULL AND resolved_at IS NULL AND resolved_by_user_id IS NULL))
);
CREATE TRIGGER guardian_identity_claim_audit BEFORE UPDATE OR DELETE ON guardian_identity_claims FOR EACH ROW EXECUTE FUNCTION protect_registration_submission();
CREATE TABLE guardian_identity_claim_candidates (
 guardian_claim_id INTEGER NOT NULL REFERENCES guardian_identity_claims(id), person_id INTEGER NOT NULL REFERENCES persons(id),
 confidence TEXT NOT NULL CHECK(confidence IN ('strong','possible','weak')),
 matched_name BOOLEAN NOT NULL, matched_birth_date BOOLEAN NOT NULL, matched_email BOOLEAN NOT NULL, matched_phone BOOLEAN NOT NULL,
 detected_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(guardian_claim_id,person_id)
);
CREATE TRIGGER guardian_identity_candidates_audit BEFORE UPDATE OR DELETE ON guardian_identity_claim_candidates FOR EACH ROW EXECUTE FUNCTION protect_registration_candidate();
CREATE TABLE child_registration_applications (
 application_id INTEGER PRIMARY KEY REFERENCES registration_applications(id),
 guardian_claim_id INTEGER NOT NULL UNIQUE REFERENCES guardian_identity_claims(id),
 relationship_type TEXT NOT NULL CHECK(relationship_type IN ('mother','father','guardian','other')),
 emergency_contact_requested BOOLEAN NOT NULL,
 guardian_confirmed_at TIMESTAMPTZ, guardian_confirmed_by_user_id INTEGER REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CHECK(guardian_confirmed_by_user_id IS NULL OR guardian_confirmed_at IS NOT NULL)
);
-- +goose StatementBegin
CREATE FUNCTION protect_child_registration() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'child registration history is retained'; END IF;
 IF OLD.guardian_confirmed_at IS NOT NULL OR
 (NEW.application_id,NEW.guardian_claim_id,NEW.relationship_type,NEW.emergency_contact_requested,NEW.created_at)
 IS DISTINCT FROM (OLD.application_id,OLD.guardian_claim_id,OLD.relationship_type,OLD.emergency_contact_requested,OLD.created_at)
 THEN RAISE EXCEPTION 'child registration declaration and confirmation are immutable'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER child_registration_audit BEFORE UPDATE OR DELETE ON child_registration_applications FOR EACH ROW EXECUTE FUNCTION protect_child_registration();
CREATE TRIGGER guardian_claim_no_truncate BEFORE TRUNCATE ON guardian_identity_claims FOR EACH STATEMENT EXECUTE FUNCTION protect_registration_candidate();
CREATE TRIGGER guardian_candidates_no_truncate BEFORE TRUNCATE ON guardian_identity_claim_candidates FOR EACH STATEMENT EXECUTE FUNCTION protect_registration_candidate();
CREATE TRIGGER child_application_no_truncate BEFORE TRUNCATE ON child_registration_applications FOR EACH STATEMENT EXECUTE FUNCTION protect_registration_candidate();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM guardian_identity_claims) OR EXISTS(SELECT 1 FROM child_registration_applications) THEN
 RAISE EXCEPTION 'cannot discard child registration staging and audit'; END IF;
END $$;
-- +goose StatementEnd
DROP TABLE child_registration_applications;
DROP FUNCTION protect_child_registration();
DROP TABLE guardian_identity_claim_candidates;
DROP TABLE guardian_identity_claims;
ALTER TABLE registration_applications DROP CONSTRAINT registration_applications_last_error_code_check;
ALTER TABLE registration_applications ADD CONSTRAINT registration_applications_last_error_code_check CHECK(last_error_code IN ('membership_already_exists','choices_unavailable','member_not_adult','membership_unavailable'));
