-- +goose Up
ALTER TABLE persons ALTER COLUMN birth_date DROP NOT NULL;
ALTER TABLE persons ADD COLUMN notes TEXT;
ALTER TABLE trial_registrations ADD COLUMN notes TEXT;

CREATE TABLE person_guardians (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    child_person_id INTEGER NOT NULL REFERENCES persons(id),
    guardian_person_id INTEGER NOT NULL REFERENCES persons(id),
    relationship_type TEXT NOT NULL CHECK (relationship_type IN ('mother', 'father', 'guardian', 'other')),
    is_primary_contact BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT person_guardians_no_self CHECK (child_person_id <> guardian_person_id),
    CONSTRAINT person_guardians_unique_pair UNIQUE (child_person_id, guardian_person_id)
);

CREATE UNIQUE INDEX person_guardians_one_primary_contact
ON person_guardians (child_person_id) WHERE is_primary_contact;

CREATE INDEX person_guardians_guardian_person_id_idx
ON person_guardians (guardian_person_id);

-- +goose Down
-- Refuse rollback if birth dates are missing; never invent dates or delete persons.
ALTER TABLE persons ALTER COLUMN birth_date SET NOT NULL;
DROP TABLE person_guardians;
ALTER TABLE trial_registrations DROP COLUMN notes;
ALTER TABLE persons DROP COLUMN notes;
