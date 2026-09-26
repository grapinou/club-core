-- +goose Up
ALTER TABLE administrative_events ADD COLUMN role_name TEXT;
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_action_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_action_check
 CHECK (action IN ('person_notes_updated','trial_scheduled','trial_rescheduled','trial_status_updated','trial_notes_updated','membership_requested','membership_notes_updated','membership_group_assigned','membership_group_closed','role_granted','role_revoked'));
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_resource_type_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_resource_type_check
 CHECK (resource_type IN ('person','trial','membership','user'));
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_role_detail_check
 CHECK ((action IN ('role_granted','role_revoked')) = (role_name IS NOT NULL));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM administrative_events WHERE action IN ('role_granted','role_revoked')) THEN
  RAISE EXCEPTION 'cannot discard role audit';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_role_detail_check;
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_action_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_action_check
 CHECK (action IN ('person_notes_updated','trial_scheduled','trial_rescheduled','trial_status_updated','trial_notes_updated','membership_requested','membership_notes_updated','membership_group_assigned','membership_group_closed'));
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_resource_type_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_resource_type_check
 CHECK (resource_type IN ('person','trial','membership'));
ALTER TABLE administrative_events DROP COLUMN role_name;
