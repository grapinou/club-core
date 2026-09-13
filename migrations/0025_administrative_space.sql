-- +goose Up
ALTER TABLE trial_registrations ADD COLUMN revision INTEGER NOT NULL DEFAULT 0;
CREATE TABLE administrative_events (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_user_id INTEGER NOT NULL REFERENCES users(id),
 action TEXT NOT NULL CHECK (action IN ('person_notes_updated','trial_scheduled','trial_rescheduled','trial_status_updated','trial_notes_updated','membership_requested','membership_notes_updated','membership_group_assigned','membership_group_closed')),
 resource_type TEXT NOT NULL CHECK (resource_type IN ('person','trial','membership')),
 resource_id INTEGER NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM administrative_events) THEN RAISE EXCEPTION 'cannot discard administrative audit'; END IF;
END $$;
-- +goose StatementEnd
DROP TABLE administrative_events;
ALTER TABLE trial_registrations DROP COLUMN revision;
