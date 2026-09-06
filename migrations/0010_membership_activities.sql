-- +goose Up
CREATE TABLE membership_activities(
    membership_id INTEGER NOT NULL REFERENCES memberships(id),
    activity_id INTEGER NOT NULL REFERENCES activities(id),
    PRIMARY KEY (membership_id, activity_id)
);

-- +goose Down
DROP TABLE membership_activities;
