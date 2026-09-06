-- +goose Up
CREATE TABLE groups (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    activity_id INTEGER NOT NULL REFERENCES activities(id),
    name TEXT NOT NULL,
    description TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (activity_id, name)
);

CREATE TABLE membership_groups (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    membership_id INTEGER NOT NULL REFERENCES memberships(id),
    group_id INTEGER NOT NULL REFERENCES groups(id),
    joined_at DATE NOT NULL,
    left_at DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (left_at IS NULL OR left_at >= joined_at)
);

CREATE UNIQUE INDEX membership_groups_unique_open_assignment
ON membership_groups (membership_id, group_id) WHERE left_at IS NULL;
CREATE INDEX membership_groups_membership_id_idx ON membership_groups (membership_id);
CREATE INDEX membership_groups_group_id_idx ON membership_groups (group_id);

CREATE TABLE group_slots (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id INTEGER NOT NULL REFERENCES groups(id),
    season_id INTEGER NOT NULL REFERENCES seasons(id),
    weekday SMALLINT NOT NULL CHECK (weekday BETWEEN 1 AND 7),
    start_time TIME NOT NULL,
    end_time TIME NOT NULL,
    location TEXT,
    valid_from DATE NOT NULL,
    valid_until DATE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (end_time > start_time),
    CHECK (valid_until IS NULL OR valid_until >= valid_from)
);
COMMENT ON COLUMN group_slots.weekday IS 'ISO weekday: 1 = Monday, 7 = Sunday';
COMMENT ON COLUMN group_slots.valid_until IS 'Inclusive last date of validity';
COMMENT ON COLUMN membership_groups.left_at IS 'First date no longer assigned; current interval is [joined_at, left_at)';
CREATE INDEX group_slots_group_season_idx ON group_slots (group_id, season_id);
CREATE INDEX group_slots_season_id_idx ON group_slots (season_id);

-- +goose Down
DROP TABLE group_slots;
DROP TABLE membership_groups;
DROP TABLE groups;
