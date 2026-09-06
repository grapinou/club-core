-- +goose Up
CREATE TABLE trial_registrations (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    person_id INTEGER NOT NULL REFERENCES persons(id),
    activity_id INTEGER NOT NULL REFERENCES activities(id),
    trial_date DATE NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('registered', 'attended', 'cancelled', 'no_show')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE trial_registrations;
