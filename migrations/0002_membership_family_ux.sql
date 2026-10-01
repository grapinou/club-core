-- +goose Up
CREATE TABLE membership_type_groups (
    membership_type_id INTEGER NOT NULL REFERENCES membership_types(id),
    group_id INTEGER NOT NULL REFERENCES groups(id),
    PRIMARY KEY (membership_type_id, group_id)
);

-- Staged adult contact. Identity resolution concerns only the applicant.
CREATE TABLE registration_application_emergency_contacts (
    application_id INTEGER PRIMARY KEY REFERENCES registration_applications(id),
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    phone_number TEXT NOT NULL,
    relationship_label TEXT NOT NULL,
    email TEXT,
    contact_person_id INTEGER REFERENCES persons(id)
);


-- Extend existing audit actions without discarding any historical event.
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

-- +goose Down
-- Fail rather than discard reference configuration or staged contact data.
CREATE TABLE family_ux_rollback_guard (n INTEGER CHECK (n = 0));
INSERT INTO family_ux_rollback_guard SELECT count(*) FROM membership_type_groups;
INSERT INTO family_ux_rollback_guard SELECT count(*) FROM registration_application_emergency_contacts;
DROP TABLE family_ux_rollback_guard;
DROP TABLE registration_application_emergency_contacts;
DROP TABLE membership_type_groups;

CREATE TABLE family_audit_rollback_guard (n INTEGER CHECK (n = 0));
INSERT INTO family_audit_rollback_guard SELECT count(*) FROM administrative_events WHERE action IN ('emergency_contact_updated','emergency_contact_removed','family_relation_saved','family_relation_removed');
DROP TABLE family_audit_rollback_guard;
CREATE TABLE administrative_events_next (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_user_id INTEGER NOT NULL,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    role_name text,
    CONSTRAINT administrative_events_action_check CHECK ((action IN ('person_notes_updated', 'trial_scheduled', 'trial_rescheduled', 'trial_status_updated', 'trial_notes_updated', 'membership_requested', 'membership_notes_updated', 'membership_group_assigned', 'membership_group_closed', 'role_granted', 'role_revoked', 'club_configuration_saved', 'emergency_contact_added', 'membership_trial_group_assigned', 'membership_trial_group_skipped', 'membership_consent_recorded'))),
    CONSTRAINT administrative_events_resource_type_check CHECK ((resource_type IN ('person', 'trial', 'membership', 'user', 'organization', 'location', 'activity', 'group', 'season', 'group_slot', 'membership_type', 'organization_link', 'organization_public_image'))),
    CONSTRAINT administrative_events_role_detail_check CHECK (((action IN ('role_granted', 'role_revoked')) = (role_name IS NOT NULL))),
    CONSTRAINT administrative_events_actor_user_id_fkey FOREIGN KEY (actor_user_id) REFERENCES users(id)
);
INSERT INTO administrative_events_next SELECT * FROM administrative_events;
DROP TABLE administrative_events;
ALTER TABLE administrative_events_next RENAME TO administrative_events;
CREATE INDEX administrative_events_membership_trial_group_idx ON administrative_events (resource_id, action) WHERE ((resource_type = 'membership') AND (action IN ('membership_trial_group_assigned', 'membership_trial_group_skipped')));
