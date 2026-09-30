-- +goose Up
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_action_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_action_check
 CHECK (action IN ('person_notes_updated','trial_scheduled','trial_rescheduled','trial_status_updated','trial_notes_updated','membership_requested','membership_notes_updated','membership_group_assigned','membership_group_closed','role_granted','role_revoked','club_configuration_saved','emergency_contact_added','membership_trial_group_assigned','membership_trial_group_skipped','membership_consent_recorded'));
-- +goose Down
-- Keep the audit trail: rollback intentionally fails while new events exist.
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_action_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_action_check
 CHECK (action IN ('person_notes_updated','trial_scheduled','trial_rescheduled','trial_status_updated','trial_notes_updated','membership_requested','membership_notes_updated','membership_group_assigned','membership_group_closed','role_granted','role_revoked','club_configuration_saved','emergency_contact_added','membership_trial_group_assigned','membership_trial_group_skipped'));
