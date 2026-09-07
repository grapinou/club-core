-- +goose Up
ALTER TABLE groups ADD CONSTRAINT groups_id_activity_unique UNIQUE (id, activity_id);
ALTER TABLE group_slots ADD CONSTRAINT group_slots_id_group_unique UNIQUE (id, group_id);

ALTER TABLE trial_registrations
    ADD COLUMN group_id INTEGER,
    ADD COLUMN group_slot_id INTEGER,
    ADD CONSTRAINT trial_slot_requires_group CHECK (group_slot_id IS NULL OR group_id IS NOT NULL),
    ADD CONSTRAINT trial_group_activity_fk FOREIGN KEY (group_id, activity_id)
        REFERENCES groups (id, activity_id),
    ADD CONSTRAINT trial_slot_group_fk FOREIGN KEY (group_slot_id, group_id)
        REFERENCES group_slots (id, group_id);

CREATE INDEX trial_registrations_person_date_idx ON trial_registrations (person_id, trial_date);
CREATE INDEX trial_registrations_date_idx ON trial_registrations (trial_date);
CREATE INDEX trial_registrations_group_idx ON trial_registrations (group_id);
CREATE INDEX trial_registrations_slot_idx ON trial_registrations (group_slot_id);

-- +goose Down
DROP INDEX trial_registrations_slot_idx;
DROP INDEX trial_registrations_group_idx;
DROP INDEX trial_registrations_date_idx;
DROP INDEX trial_registrations_person_date_idx;
ALTER TABLE trial_registrations
    DROP CONSTRAINT trial_slot_group_fk,
    DROP CONSTRAINT trial_group_activity_fk,
    DROP CONSTRAINT trial_slot_requires_group,
    DROP COLUMN group_slot_id,
    DROP COLUMN group_id;
ALTER TABLE group_slots DROP CONSTRAINT group_slots_id_group_unique;
ALTER TABLE groups DROP CONSTRAINT groups_id_activity_unique;
