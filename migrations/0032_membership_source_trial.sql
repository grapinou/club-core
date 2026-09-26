-- +goose Up
-- Historical memberships remain direct/unspecified: no origin is inferred.
ALTER TABLE memberships ADD COLUMN source_trial_id INTEGER
    REFERENCES trial_registrations(id);
CREATE UNIQUE INDEX memberships_source_trial_unique ON memberships(source_trial_id)
    WHERE source_trial_id IS NOT NULL;

ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_action_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_action_check
 CHECK (action IN ('person_notes_updated','trial_scheduled','trial_rescheduled','trial_status_updated','trial_notes_updated','membership_requested','membership_notes_updated','membership_group_assigned','membership_group_closed','role_granted','role_revoked','club_configuration_saved','emergency_contact_added'));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM memberships WHERE source_trial_id IS NOT NULL)
 OR EXISTS(SELECT 1 FROM administrative_events WHERE action='emergency_contact_added') THEN
  RAISE EXCEPTION 'cannot discard membership origins or emergency contact audit';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_action_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_action_check
 CHECK (action IN ('person_notes_updated','trial_scheduled','trial_rescheduled','trial_status_updated','trial_notes_updated','membership_requested','membership_notes_updated','membership_group_assigned','membership_group_closed','role_granted','role_revoked','club_configuration_saved'));
ALTER TABLE memberships DROP COLUMN source_trial_id;
