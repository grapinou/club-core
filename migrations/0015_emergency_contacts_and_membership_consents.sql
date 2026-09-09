-- +goose Up
CREATE TABLE person_emergency_contacts (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    person_id INTEGER NOT NULL REFERENCES persons(id),
    contact_person_id INTEGER NOT NULL REFERENCES persons(id),
    relationship_label TEXT,
    priority INTEGER NOT NULL CHECK (priority > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (person_id <> contact_person_id),
    UNIQUE (person_id, contact_person_id),
    UNIQUE (person_id, priority)
);
CREATE INDEX person_emergency_contacts_contact_idx ON person_emergency_contacts(contact_person_id);

CREATE TABLE consent_definitions (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code TEXT NOT NULL CHECK (btrim(code) <> ''),
    version INTEGER NOT NULL CHECK (version > 0),
    title TEXT NOT NULL CHECK (btrim(title) <> ''),
    description TEXT NOT NULL CHECK (btrim(description) <> ''),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (code, version)
);

CREATE UNIQUE INDEX consent_definitions_one_active_code
ON consent_definitions(code) WHERE is_active;

CREATE TABLE membership_consents (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    membership_id INTEGER NOT NULL REFERENCES memberships(id),
    consent_definition_id INTEGER NOT NULL REFERENCES consent_definitions(id),
    decision TEXT NOT NULL CHECK (decision IN ('granted', 'refused', 'withdrawn')),
    given_by_person_id INTEGER NOT NULL REFERENCES persons(id),
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX membership_consents_history_idx
ON membership_consents(membership_id, consent_definition_id, recorded_at DESC, id DESC);
CREATE INDEX membership_consents_definition_idx ON membership_consents(consent_definition_id);
CREATE INDEX membership_consents_given_by_idx ON membership_consents(given_by_person_id);

-- Preserve legal wording and decision evidence even for direct SQL writes.
-- +goose StatementBegin
CREATE FUNCTION protect_consent_definition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'consent definitions must be deactivated, not deleted' USING ERRCODE = '23514';
    END IF;
    IF (NEW.id, NEW.code, NEW.version, NEW.title, NEW.description, NEW.created_at)
       IS DISTINCT FROM (OLD.id, OLD.code, OLD.version, OLD.title, OLD.description, OLD.created_at) THEN
        RAISE EXCEPTION 'create a new consent definition version' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER consent_definitions_immutable BEFORE UPDATE OR DELETE ON consent_definitions
FOR EACH ROW EXECUTE FUNCTION protect_consent_definition();

-- +goose StatementBegin
CREATE FUNCTION protect_membership_consent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'membership consents are append-only' USING ERRCODE = '23514';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER membership_consents_append_only BEFORE UPDATE OR DELETE ON membership_consents
FOR EACH ROW EXECUTE FUNCTION protect_membership_consent();

-- +goose Down
DROP TABLE membership_consents;
DROP FUNCTION protect_membership_consent();
DROP TABLE consent_definitions;
DROP FUNCTION protect_consent_definition();
DROP TABLE person_emergency_contacts;
