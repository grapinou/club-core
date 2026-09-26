-- +goose Up
ALTER TABLE membership_types
 ADD COLUMN amount_cents INTEGER CHECK (amount_cents >= 0),
 ADD COLUMN currency TEXT NOT NULL DEFAULT 'EUR' CHECK (currency ~ '^[A-Z]{3}$'),
 ADD COLUMN public_note TEXT;
ALTER TABLE organizations ADD COLUMN public_rules_description TEXT;

ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_action_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_action_check
 CHECK (action IN ('person_notes_updated','trial_scheduled','trial_rescheduled','trial_status_updated','trial_notes_updated','membership_requested','membership_notes_updated','membership_group_assigned','membership_group_closed','role_granted','role_revoked','club_configuration_saved'));
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_resource_type_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_resource_type_check
 CHECK (resource_type IN ('person','trial','membership','user','organization','location','activity','group','season','group_slot','membership_type','organization_link','organization_public_image'));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM administrative_events WHERE action='club_configuration_saved') THEN
  RAISE EXCEPTION 'cannot discard club configuration audit';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_action_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_action_check
 CHECK (action IN ('person_notes_updated','trial_scheduled','trial_rescheduled','trial_status_updated','trial_notes_updated','membership_requested','membership_notes_updated','membership_group_assigned','membership_group_closed','role_granted','role_revoked'));
ALTER TABLE administrative_events DROP CONSTRAINT administrative_events_resource_type_check;
ALTER TABLE administrative_events ADD CONSTRAINT administrative_events_resource_type_check
 CHECK (resource_type IN ('person','trial','membership','user'));
ALTER TABLE membership_types DROP COLUMN amount_cents, DROP COLUMN currency, DROP COLUMN public_note;
ALTER TABLE organizations DROP COLUMN public_rules_description;
