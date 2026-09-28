-- +goose Up
-- Only new conversions emit these events. Existing memberships are untouched.
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_action_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_action_check
 CHECK (action IN ('person_notes_updated','trial_scheduled','trial_rescheduled','trial_status_updated','trial_notes_updated','membership_requested','membership_notes_updated','membership_group_assigned','membership_group_closed','role_granted','role_revoked','club_configuration_saved','emergency_contact_added','membership_trial_group_assigned','membership_trial_group_skipped'));
CREATE INDEX administrative_events_membership_trial_group_idx
 ON administrative_events(resource_id,action)
 WHERE resource_type='membership' AND action IN ('membership_trial_group_assigned','membership_trial_group_skipped');

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM administrative_events WHERE action IN ('membership_trial_group_assigned','membership_trial_group_skipped')) THEN
  RAISE EXCEPTION 'cannot discard trial group conversion audit';
 END IF;
END $$;
-- +goose StatementEnd
DROP INDEX administrative_events_membership_trial_group_idx;
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_action_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_action_check
 CHECK (action IN ('person_notes_updated','trial_scheduled','trial_rescheduled','trial_status_updated','trial_notes_updated','membership_requested','membership_notes_updated','membership_group_assigned','membership_group_closed','role_granted','role_revoked','club_configuration_saved','emergency_contact_added'));
