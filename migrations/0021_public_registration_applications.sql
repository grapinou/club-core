-- +goose Up
CREATE TABLE registration_applications (
 id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 submission_id INTEGER NOT NULL UNIQUE REFERENCES registration_submissions(id),
 request_key BYTEA NOT NULL UNIQUE CHECK(octet_length(request_key)=32),
 season_id INTEGER NOT NULL REFERENCES seasons(id),
 membership_type_id INTEGER NOT NULL REFERENCES membership_types(id),
 status TEXT NOT NULL DEFAULT 'awaiting_identity' CHECK(status IN ('awaiting_identity','membership_created','needs_review','cancelled')),
 membership_id INTEGER UNIQUE REFERENCES memberships(id),
 last_error_code TEXT CHECK(last_error_code IN ('membership_already_exists','choices_unavailable','member_not_adult','membership_unavailable')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 finalized_at TIMESTAMPTZ,
 CHECK((status='membership_created')=(membership_id IS NOT NULL)),
 CHECK((status='membership_created')=(finalized_at IS NOT NULL)),
 CHECK((status='needs_review')=(last_error_code IS NOT NULL))
);
CREATE INDEX registration_applications_review ON registration_applications(status,id);
CREATE TABLE registration_application_activities (
 application_id INTEGER NOT NULL REFERENCES registration_applications(id),
 activity_id INTEGER NOT NULL REFERENCES activities(id),
 PRIMARY KEY(application_id,activity_id)
);
CREATE TABLE registration_application_consents (
 application_id INTEGER NOT NULL REFERENCES registration_applications(id),
 consent_definition_id INTEGER NOT NULL REFERENCES consent_definitions(id),
 decision TEXT NOT NULL CHECK(decision IN ('granted','refused')),
 presented_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(application_id,consent_definition_id)
);
-- +goose StatementBegin
CREATE FUNCTION protect_registration_application() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'registration application history is retained'; END IF;
 IF OLD.status IN ('membership_created','cancelled') THEN RAISE EXCEPTION 'registration application is final'; END IF;
 IF (NEW.id,NEW.submission_id,NEW.request_key,NEW.season_id,NEW.membership_type_id,NEW.created_at)
 IS DISTINCT FROM (OLD.id,OLD.submission_id,OLD.request_key,OLD.season_id,OLD.membership_type_id,OLD.created_at) THEN
  RAISE EXCEPTION 'registration application intent is immutable';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER registration_application_audit BEFORE UPDATE OR DELETE ON registration_applications
 FOR EACH ROW EXECUTE FUNCTION protect_registration_application();
CREATE TRIGGER registration_application_activities_audit BEFORE UPDATE OR DELETE ON registration_application_activities
 FOR EACH ROW EXECUTE FUNCTION protect_membership_consent();
CREATE TRIGGER registration_application_consents_audit BEFORE UPDATE OR DELETE ON registration_application_consents
 FOR EACH ROW EXECUTE FUNCTION protect_membership_consent();
-- Require at least one activity at commit, while allowing atomic staged inserts.
-- +goose StatementBegin
CREATE FUNCTION check_registration_application_activities() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM registration_application_activities WHERE application_id=NEW.id) THEN
  RAISE EXCEPTION 'registration application requires activity';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER registration_application_activity_required AFTER INSERT ON registration_applications
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_registration_application_activities();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM registration_applications) THEN RAISE EXCEPTION 'cannot discard public registration history'; END IF;
END $$;
-- +goose StatementEnd
DROP TABLE registration_application_consents;
DROP TABLE registration_application_activities;
DROP TABLE registration_applications;
DROP FUNCTION protect_registration_application();
DROP FUNCTION check_registration_application_activities();
