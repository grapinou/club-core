-- +goose Up
CREATE TABLE memberships (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    person_id INTEGER NOT NULL REFERENCES persons(id),
    season_id INTEGER NOT NULL REFERENCES seasons(id),
    membership_type_id INTEGER NOT NULL REFERENCES membership_types(id),
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'ended', 'cancelled')),
    joined_at DATE,
    ended_at DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(person_id, season_id)
);

-- +goose Down
DROP TABLE memberships;
