-- +goose Up
CREATE TABLE identity_correction_requests (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 person_id INTEGER NOT NULL REFERENCES persons(id),
 requesting_user_id INTEGER NOT NULL REFERENCES users(id),
 original_first_name TEXT NOT NULL,
 original_last_name TEXT NOT NULL,
 original_birth_date date,
 proposed_first_name TEXT NOT NULL,
 proposed_last_name TEXT NOT NULL,
 proposed_birth_date date CHECK (proposed_birth_date IS NULL OR (length(proposed_birth_date)=10 AND date(proposed_birth_date,'+0 days') IS proposed_birth_date)),
 status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
 created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')),
 reviewed_at DATETIME,
 reviewed_by_user_id INTEGER REFERENCES users(id),
 CHECK ((status='pending' AND reviewed_at IS NULL AND reviewed_by_user_id IS NULL)
 OR (status IN ('approved','rejected') AND reviewed_at IS NOT NULL AND reviewed_by_user_id IS NOT NULL))
);
CREATE UNIQUE INDEX identity_correction_one_pending ON identity_correction_requests(person_id) WHERE status='pending';
-- The proposal and initial identity remain evidence after review.
-- +goose StatementBegin
CREATE TRIGGER identity_correction_immutable BEFORE UPDATE ON identity_correction_requests
WHEN OLD.status <> 'pending' OR NEW.id IS NOT OLD.id OR NEW.person_id IS NOT OLD.person_id
 OR NEW.requesting_user_id IS NOT OLD.requesting_user_id OR NEW.created_at IS NOT OLD.created_at
 OR NEW.original_first_name IS NOT OLD.original_first_name OR NEW.original_last_name IS NOT OLD.original_last_name
 OR NEW.original_birth_date IS NOT OLD.original_birth_date OR NEW.proposed_first_name IS NOT OLD.proposed_first_name
 OR NEW.proposed_last_name IS NOT OLD.proposed_last_name OR NEW.proposed_birth_date IS NOT OLD.proposed_birth_date
BEGIN SELECT RAISE(ABORT, 'identity correction evidence is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER identity_correction_retained BEFORE DELETE ON identity_correction_requests
BEGIN SELECT RAISE(ABORT, 'identity correction history is retained'); END;
-- +goose StatementEnd

CREATE TABLE administrative_events_next (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_user_id INTEGER NOT NULL,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    role_name text,
    CONSTRAINT administrative_events_action_check CHECK ((action IN ('person_notes_updated', 'trial_scheduled', 'trial_rescheduled', 'trial_status_updated', 'trial_notes_updated', 'membership_requested', 'membership_notes_updated', 'membership_group_assigned', 'membership_group_closed', 'role_granted', 'role_revoked', 'club_configuration_saved', 'emergency_contact_added','emergency_contact_updated','emergency_contact_removed','family_relation_saved','family_relation_removed', 'membership_trial_group_assigned', 'membership_trial_group_skipped', 'membership_consent_recorded', 'person_identity_corrected', 'person_identity_correction_rejected'))),
    CONSTRAINT administrative_events_resource_type_check CHECK ((resource_type IN ('person', 'trial', 'membership', 'user', 'organization', 'location', 'activity', 'group', 'season', 'group_slot', 'membership_type', 'organization_link', 'organization_public_image'))),
    CONSTRAINT administrative_events_role_detail_check CHECK (((action IN ('role_granted', 'role_revoked')) = (role_name IS NOT NULL))),
    CONSTRAINT administrative_events_actor_user_id_fkey FOREIGN KEY (actor_user_id) REFERENCES users(id)
);
INSERT INTO administrative_events_next SELECT * FROM administrative_events;
DROP TABLE administrative_events;
ALTER TABLE administrative_events_next RENAME TO administrative_events;
CREATE INDEX administrative_events_membership_trial_group_idx ON administrative_events (resource_id, action) WHERE ((resource_type = 'membership') AND (action IN ('membership_trial_group_assigned', 'membership_trial_group_skipped')));

-- +goose Down
-- Never discard requests or review audit during rollback.
CREATE TABLE identity_correction_rollback_guard (n INTEGER CHECK(n=0));
INSERT INTO identity_correction_rollback_guard SELECT count(*) FROM identity_correction_requests;
INSERT INTO identity_correction_rollback_guard SELECT count(*) FROM administrative_events WHERE action IN ('person_identity_corrected','person_identity_correction_rejected');
DROP TABLE identity_correction_rollback_guard;
DROP TABLE identity_correction_requests;
CREATE TABLE administrative_events_next (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_user_id INTEGER NOT NULL,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    role_name text,
    CONSTRAINT administrative_events_action_check CHECK ((action IN ('person_notes_updated', 'trial_scheduled', 'trial_rescheduled', 'trial_status_updated', 'trial_notes_updated', 'membership_requested', 'membership_notes_updated', 'membership_group_assigned', 'membership_group_closed', 'role_granted', 'role_revoked', 'club_configuration_saved', 'emergency_contact_added','emergency_contact_updated','emergency_contact_removed','family_relation_saved','family_relation_removed', 'membership_trial_group_assigned', 'membership_trial_group_skipped', 'membership_consent_recorded'))),
    CONSTRAINT administrative_events_resource_type_check CHECK ((resource_type IN ('person', 'trial', 'membership', 'user', 'organization', 'location', 'activity', 'group', 'season', 'group_slot', 'membership_type', 'organization_link', 'organization_public_image'))),
    CONSTRAINT administrative_events_role_detail_check CHECK (((action IN ('role_granted', 'role_revoked')) = (role_name IS NOT NULL))),
    CONSTRAINT administrative_events_actor_user_id_fkey FOREIGN KEY (actor_user_id) REFERENCES users(id)
);
INSERT INTO administrative_events_next SELECT * FROM administrative_events;
DROP TABLE administrative_events;
ALTER TABLE administrative_events_next RENAME TO administrative_events;
CREATE INDEX administrative_events_membership_trial_group_idx ON administrative_events (resource_id, action) WHERE ((resource_type = 'membership') AND (action IN ('membership_trial_group_assigned', 'membership_trial_group_skipped')));
