-- +goose Up
CREATE TABLE organizations (
 id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 name TEXT NOT NULL CHECK (btrim(name) <> ''),
 short_name TEXT,
 description TEXT,
 public_email TEXT CHECK (public_email IS NULL OR public_email ~ '^[^[:space:]@]+@[^[:space:]@]+[.][^[:space:]@]+$'),
 public_phone TEXT CHECK (public_phone IS NULL OR btrim(public_phone) <> ''),
 correspondence_address TEXT,
 website_url TEXT CHECK (website_url IS NULL OR website_url ~ '^https?://[^[:space:]/?#@]+([/?#][^[:space:]]*)?$'),
 is_active BOOLEAN NOT NULL DEFAULT TRUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Single active organization for this application, not tenant isolation.
CREATE UNIQUE INDEX organizations_one_active ON organizations ((true)) WHERE is_active;
CREATE TABLE locations (
 id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 organization_id INTEGER NOT NULL REFERENCES organizations(id),
 name TEXT NOT NULL CHECK (btrim(name) <> ''),
 address TEXT NOT NULL CHECK (btrim(address) <> ''),
 is_active BOOLEAN NOT NULL DEFAULT TRUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE (organization_id, name)
);
CREATE TABLE organization_links (
 id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 organization_id INTEGER NOT NULL REFERENCES organizations(id),
 kind TEXT NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_-]*$'),
 label TEXT NOT NULL CHECK (btrim(label) <> ''),
 url TEXT NOT NULL CHECK (url ~ '^https?://[^[:space:]/?#@]+([/?#][^[:space:]]*)?$'),
 position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
 is_active BOOLEAN NOT NULL DEFAULT TRUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE (organization_id, url)
);
ALTER TABLE group_slots ADD COLUMN location_id INTEGER REFERENCES locations(id),
 ADD COLUMN practice_label TEXT CHECK (practice_label IS NULL OR btrim(practice_label) <> ''),
 ADD CONSTRAINT group_slots_one_location CHECK (location_id IS NULL OR location IS NULL);
CREATE INDEX group_slots_location_idx ON group_slots(location_id);
COMMENT ON COLUMN group_slots.location IS 'Legacy free text; mutually exclusive with location_id. New reference data uses locations.';
COMMENT ON COLUMN group_slots.practice_label IS 'Optional session designation, not an activity or eligibility rule.';

-- +goose Down
ALTER TABLE group_slots DROP CONSTRAINT group_slots_one_location, DROP COLUMN location_id, DROP COLUMN practice_label;
DROP TABLE organization_links;
DROP TABLE locations;
DROP TABLE organizations;
