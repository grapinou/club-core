-- +goose Up
-- SQLite baseline: final pre-production schema through P4.3.2 (former version 35).

CREATE TABLE account_security_events (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    person_id INTEGER NOT NULL,
    event text NOT NULL,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT account_security_events_event_check CHECK ((event IN ('email_change_requested', 'email_changed', 'password_changed', 'profile_contact_updated'))),
    CONSTRAINT account_security_events_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id),
    CONSTRAINT account_security_events_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE activities (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name text NOT NULL,
    is_active boolean DEFAULT true NOT NULL CHECK (is_active IN (0,1)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT activities_name_key UNIQUE (name)
);

CREATE TABLE administrative_events (

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

CREATE TABLE child_registration_applications (

    application_id INTEGER NOT NULL,
    guardian_claim_id INTEGER NOT NULL,
    relationship_type text NOT NULL,
    emergency_contact_requested boolean NOT NULL CHECK (emergency_contact_requested IN (0,1)),
    guardian_confirmed_at DATETIME,
    guardian_confirmed_by_user_id INTEGER,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT child_registration_applications_check CHECK (((guardian_confirmed_by_user_id IS NULL) OR (guardian_confirmed_at IS NOT NULL))),
    CONSTRAINT child_registration_applications_relationship_type_check CHECK ((relationship_type IN ('mother', 'father', 'guardian', 'other'))),
    CONSTRAINT child_registration_applications_guardian_claim_id_key UNIQUE (guardian_claim_id),
    CONSTRAINT child_registration_applications_pkey PRIMARY KEY (application_id),
    CONSTRAINT child_registration_applicatio_guardian_confirmed_by_user_i_fkey FOREIGN KEY (guardian_confirmed_by_user_id) REFERENCES users(id),
    CONSTRAINT child_registration_applications_application_id_fkey FOREIGN KEY (application_id) REFERENCES registration_applications(id),
    CONSTRAINT child_registration_applications_guardian_claim_id_fkey FOREIGN KEY (guardian_claim_id) REFERENCES guardian_identity_claims(id)
);

CREATE TABLE consent_definitions (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code text NOT NULL,
    version INTEGER NOT NULL,
    title text NOT NULL,
    description text NOT NULL,
    is_active boolean DEFAULT true NOT NULL CHECK (is_active IN (0,1)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT consent_definitions_code_check CHECK ((trim(code) <> '')),
    CONSTRAINT consent_definitions_description_check CHECK ((trim(description) <> '')),
    CONSTRAINT consent_definitions_title_check CHECK ((trim(title) <> '')),
    CONSTRAINT consent_definitions_version_check CHECK ((version > 0)),
    CONSTRAINT consent_definitions_code_version_key UNIQUE (code, version)
);

CREATE TABLE group_slots (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL,
    season_id INTEGER NOT NULL,
    weekday smallint NOT NULL,
    start_time TIME NOT NULL CHECK (start_time IS NULL OR time(start_time) IS NOT NULL),
    end_time TIME NOT NULL CHECK (end_time IS NULL OR time(end_time) IS NOT NULL),
    location text,
    valid_from date NOT NULL CHECK (valid_from IS NULL OR (length(valid_from)=10 AND valid_from BETWEEN '0001-01-01' AND '9999-12-31' AND date(valid_from,'+0 days') IS valid_from)),
    valid_until date CHECK (valid_until IS NULL OR (length(valid_until)=10 AND valid_until BETWEEN '0001-01-01' AND '9999-12-31' AND date(valid_until,'+0 days') IS valid_until)),
    is_active boolean DEFAULT true NOT NULL CHECK (is_active IN (0,1)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    location_id INTEGER,
    practice_label text,
    CONSTRAINT group_slots_check CHECK (julianday('2000-01-01 '||end_time) > julianday('2000-01-01 '||start_time)),
    CONSTRAINT group_slots_check1 CHECK (((valid_until IS NULL) OR (valid_until >= valid_from))),
    CONSTRAINT group_slots_one_location CHECK (((location_id IS NULL) OR (location IS NULL))),
    CONSTRAINT group_slots_practice_label_check CHECK (((practice_label IS NULL) OR (trim(practice_label) <> ''))),
    CONSTRAINT group_slots_weekday_check CHECK (((weekday >= 1) AND (weekday <= 7))),
    CONSTRAINT group_slots_id_group_unique UNIQUE (id, group_id),
    CONSTRAINT group_slots_group_id_fkey FOREIGN KEY (group_id) REFERENCES groups(id),
    CONSTRAINT group_slots_location_id_fkey FOREIGN KEY (location_id) REFERENCES locations(id),
    CONSTRAINT group_slots_season_id_fkey FOREIGN KEY (season_id) REFERENCES seasons(id)
);

CREATE TABLE groups (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activity_id INTEGER NOT NULL,
    name text NOT NULL,
    description text,
    is_active boolean DEFAULT true NOT NULL CHECK (is_active IN (0,1)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    show_name_publicly boolean DEFAULT true NOT NULL CHECK (show_name_publicly IN (0,1)),
    CONSTRAINT groups_activity_id_name_key UNIQUE (activity_id, name),
    CONSTRAINT groups_id_activity_unique UNIQUE (id, activity_id),
    CONSTRAINT groups_activity_id_fkey FOREIGN KEY (activity_id) REFERENCES activities(id)
);

CREATE TABLE guardian_access_grants (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    child_person_id INTEGER NOT NULL,
    guardian_person_id INTEGER NOT NULL,
    granted_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    granted_by_user_id INTEGER,
    revoked_at DATETIME,
    revoked_by_user_id INTEGER,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT guardian_access_grants_check CHECK ((child_person_id <> guardian_person_id)),
    CONSTRAINT guardian_access_grants_check1 CHECK (((revoked_at IS NOT NULL) OR (revoked_by_user_id IS NULL))),
    CONSTRAINT guardian_access_grants_check2 CHECK (((revoked_at IS NULL) OR (revoked_at >= granted_at))),
    CONSTRAINT guardian_access_grants_child_person_id_fkey FOREIGN KEY (child_person_id) REFERENCES persons(id),
    CONSTRAINT guardian_access_grants_granted_by_user_id_fkey FOREIGN KEY (granted_by_user_id) REFERENCES users(id),
    CONSTRAINT guardian_access_grants_guardian_person_id_fkey FOREIGN KEY (guardian_person_id) REFERENCES persons(id),
    CONSTRAINT guardian_access_grants_revoked_by_user_id_fkey FOREIGN KEY (revoked_by_user_id) REFERENCES users(id)
);

CREATE TABLE guardian_identity_claim_candidates (

    guardian_claim_id INTEGER NOT NULL,
    person_id INTEGER NOT NULL,
    confidence text NOT NULL,
    matched_name boolean NOT NULL CHECK (matched_name IN (0,1)),
    matched_birth_date boolean NOT NULL CHECK (matched_birth_date IN (0,1)),
    matched_email boolean NOT NULL CHECK (matched_email IN (0,1)),
    matched_phone boolean NOT NULL CHECK (matched_phone IN (0,1)),
    detected_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT guardian_identity_claim_candidates_confidence_check CHECK ((confidence IN ('strong', 'possible', 'weak'))),
    CONSTRAINT guardian_identity_claim_candidates_pkey PRIMARY KEY (guardian_claim_id, person_id),
    CONSTRAINT guardian_identity_claim_candidates_guardian_claim_id_fkey FOREIGN KEY (guardian_claim_id) REFERENCES guardian_identity_claims(id),
    CONSTRAINT guardian_identity_claim_candidates_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id)
);

CREATE TABLE guardian_identity_claims (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status text DEFAULT 'received' NOT NULL,
    first_name text NOT NULL,
    last_name text NOT NULL,
    birth_date date CHECK (birth_date IS NULL OR (length(birth_date)=10 AND birth_date BETWEEN '0001-01-01' AND '9999-12-31' AND date(birth_date,'+0 days') IS birth_date)),
    email text NOT NULL,
    phone_number text,
    address text,
    resolved_person_id INTEGER,
    resolution_type text,
    resolved_at DATETIME,
    resolved_by_user_id INTEGER,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT guardian_identity_claims_check CHECK ((((status = 'resolved') AND (resolved_person_id IS NOT NULL) AND (resolution_type IS NOT NULL) AND (resolved_at IS NOT NULL)) OR ((status <> 'resolved') AND (resolved_person_id IS NULL) AND (resolution_type IS NULL) AND (resolved_at IS NULL) AND (resolved_by_user_id IS NULL)))),
    CONSTRAINT guardian_identity_claims_email_check CHECK ((trim(email) <> '')),
    CONSTRAINT guardian_identity_claims_first_name_check CHECK ((trim(first_name) <> '')),
    CONSTRAINT guardian_identity_claims_last_name_check CHECK ((trim(last_name) <> '')),
    CONSTRAINT guardian_identity_claims_resolution_type_check CHECK ((resolution_type IN ('existing_person', 'new_person'))),
    CONSTRAINT guardian_identity_claims_status_check CHECK ((status IN ('received', 'awaiting_review', 'resolved', 'cancelled'))),
    CONSTRAINT guardian_identity_claims_resolved_by_user_id_fkey FOREIGN KEY (resolved_by_user_id) REFERENCES users(id),
    CONSTRAINT guardian_identity_claims_resolved_person_id_fkey FOREIGN KEY (resolved_person_id) REFERENCES persons(id)
);

CREATE TABLE installation_setup (

    id boolean DEFAULT true NOT NULL CHECK (id IN (0,1)),
    initialized_at DATETIME,
    first_admin_user_id INTEGER,
    secret_hash BLOB,
    secret_issued_at DATETIME,
    secret_generation INTEGER DEFAULT 0 NOT NULL,
    CONSTRAINT installation_setup_check CHECK (((initialized_at IS NULL) OR (secret_hash IS NULL))),
    CONSTRAINT installation_setup_check1 CHECK (((secret_hash IS NULL) = (secret_issued_at IS NULL))),
    CONSTRAINT installation_setup_id_check CHECK (id),
    CONSTRAINT installation_setup_secret_generation_check CHECK ((secret_generation >= 0)),
    CONSTRAINT installation_setup_secret_hash_check CHECK (((secret_hash IS NULL) OR (length(secret_hash) = 32))),
    CONSTRAINT installation_setup_pkey PRIMARY KEY (id),
    CONSTRAINT installation_setup_first_admin_user_id_fkey FOREIGN KEY (first_admin_user_id) REFERENCES users(id)
);

CREATE TABLE locations (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    organization_id INTEGER NOT NULL,
    name text NOT NULL,
    address text NOT NULL,
    is_active boolean DEFAULT true NOT NULL CHECK (is_active IN (0,1)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT locations_address_check CHECK ((trim(address) <> '')),
    CONSTRAINT locations_name_check CHECK ((trim(name) <> '')),
    CONSTRAINT locations_organization_id_name_key UNIQUE (organization_id, name),
    CONSTRAINT locations_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations(id)
);

CREATE TABLE membership_activities (

    membership_id INTEGER NOT NULL,
    activity_id INTEGER NOT NULL,
    CONSTRAINT membership_activities_pkey PRIMARY KEY (membership_id, activity_id),
    CONSTRAINT membership_activities_activity_id_fkey FOREIGN KEY (activity_id) REFERENCES activities(id),
    CONSTRAINT membership_activities_membership_id_fkey FOREIGN KEY (membership_id) REFERENCES memberships(id)
);

CREATE TABLE membership_consent_requirements (

    membership_id INTEGER NOT NULL,
    consent_definition_id INTEGER NOT NULL,
    presented_at DATETIME NOT NULL,
    CONSTRAINT membership_consent_requirements_pkey PRIMARY KEY (membership_id, consent_definition_id),
    CONSTRAINT membership_consent_requirements_consent_definition_id_fkey FOREIGN KEY (consent_definition_id) REFERENCES consent_definitions(id),
    CONSTRAINT membership_consent_requirements_membership_id_fkey FOREIGN KEY (membership_id) REFERENCES memberships(id)
);

CREATE TABLE membership_consents (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    membership_id INTEGER NOT NULL,
    consent_definition_id INTEGER NOT NULL,
    decision text NOT NULL,
    given_by_person_id INTEGER NOT NULL,
    recorded_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT membership_consents_decision_check CHECK ((decision IN ('granted', 'refused', 'withdrawn'))),
    CONSTRAINT membership_consents_consent_definition_id_fkey FOREIGN KEY (consent_definition_id) REFERENCES consent_definitions(id),
    CONSTRAINT membership_consents_given_by_person_id_fkey FOREIGN KEY (given_by_person_id) REFERENCES persons(id),
    CONSTRAINT membership_consents_membership_id_fkey FOREIGN KEY (membership_id) REFERENCES memberships(id)
);

CREATE TABLE membership_groups (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    membership_id INTEGER NOT NULL,
    group_id INTEGER NOT NULL,
    joined_at date NOT NULL CHECK (joined_at IS NULL OR (length(joined_at)=10 AND joined_at BETWEEN '0001-01-01' AND '9999-12-31' AND date(joined_at,'+0 days') IS joined_at)),
    left_at date CHECK (left_at IS NULL OR (length(left_at)=10 AND left_at BETWEEN '0001-01-01' AND '9999-12-31' AND date(left_at,'+0 days') IS left_at)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT membership_groups_check CHECK (((left_at IS NULL) OR (left_at >= joined_at))),
    CONSTRAINT membership_groups_group_id_fkey FOREIGN KEY (group_id) REFERENCES groups(id),
    CONSTRAINT membership_groups_membership_id_fkey FOREIGN KEY (membership_id) REFERENCES memberships(id)
);

CREATE TABLE membership_types (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name text NOT NULL,
    is_active boolean DEFAULT true NOT NULL CHECK (is_active IN (0,1)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    amount_cents INTEGER,
    currency text DEFAULT 'EUR' NOT NULL,
    public_note text,
    CONSTRAINT membership_types_amount_cents_check CHECK ((amount_cents >= 0)),
    CONSTRAINT membership_types_currency_check CHECK ((length(currency)=3 AND currency NOT GLOB '*[^A-Z]*')),
    CONSTRAINT membership_types_name_key UNIQUE (name)
);

CREATE TABLE memberships (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id INTEGER NOT NULL,
    season_id INTEGER NOT NULL,
    membership_type_id INTEGER NOT NULL,
    status text NOT NULL,
    joined_at date CHECK (joined_at IS NULL OR (length(joined_at)=10 AND joined_at BETWEEN '0001-01-01' AND '9999-12-31' AND date(joined_at,'+0 days') IS joined_at)),
    ended_at date CHECK (ended_at IS NULL OR (length(ended_at)=10 AND ended_at BETWEEN '0001-01-01' AND '9999-12-31' AND date(ended_at,'+0 days') IS ended_at)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    requested_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    approved_at DATETIME,
    approved_by_user_id INTEGER,
    admin_note text,
    source_trial_id INTEGER,
    CONSTRAINT memberships_status_check CHECK ((status IN ('pending', 'active', 'ended', 'cancelled'))),
    CONSTRAINT memberships_person_id_season_id_key UNIQUE (person_id, season_id),
    CONSTRAINT memberships_approved_by_user_id_fkey FOREIGN KEY (approved_by_user_id) REFERENCES users(id),
    CONSTRAINT memberships_membership_type_id_fkey FOREIGN KEY (membership_type_id) REFERENCES membership_types(id),
    CONSTRAINT memberships_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id),
    CONSTRAINT memberships_season_id_fkey FOREIGN KEY (season_id) REFERENCES seasons(id),
    CONSTRAINT memberships_source_trial_id_fkey FOREIGN KEY (source_trial_id) REFERENCES trial_registrations(id)
);

CREATE TABLE organization_links (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    organization_id INTEGER NOT NULL,
    kind text NOT NULL,
    label text NOT NULL,
    url text NOT NULL,
    "position" INTEGER DEFAULT 0 NOT NULL,
    is_active boolean DEFAULT true NOT NULL CHECK (is_active IN (0,1)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT organization_links_kind_check CHECK ((valid_link_kind(kind))),
    CONSTRAINT organization_links_label_check CHECK ((trim(label) <> '')),
    CONSTRAINT organization_links_position_check CHECK (("position" >= 0)),
    CONSTRAINT organization_links_url_check CHECK ((valid_public_url(url))),
    CONSTRAINT organization_links_organization_id_url_key UNIQUE (organization_id, url),
    CONSTRAINT organization_links_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations(id)
);

CREATE TABLE organization_public_images (

    organization_id INTEGER NOT NULL,
    placement text NOT NULL,
    src text NOT NULL,
    webp_srcset text DEFAULT '' NOT NULL,
    alt text DEFAULT '' NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    CONSTRAINT organization_public_images_height_check CHECK ((height > 0)),
    CONSTRAINT organization_public_images_placement_check CHECK ((placement IN ('hero', 'activity', 'community', 'schedule', 'trial'))),
    CONSTRAINT organization_public_images_src_check CHECK ((src LIKE '/static/%')),
    CONSTRAINT organization_public_images_width_check CHECK ((width > 0)),
    CONSTRAINT organization_public_images_pkey PRIMARY KEY (organization_id, placement),
    CONSTRAINT organization_public_images_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE
);

CREATE TABLE organizations (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name text NOT NULL,
    short_name text,
    description text,
    public_email text,
    public_phone text,
    correspondence_address text,
    website_url text,
    is_active boolean DEFAULT true NOT NULL CHECK (is_active IN (0,1)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    public_phone_label text,
    trial_session_description text,
    trial_items_to_bring TEXT DEFAULT '[]' NOT NULL CHECK(json_valid(trial_items_to_bring) AND json_type(trial_items_to_bring)='array'),
    trial_equipment_offer text,
    trial_equipment_detail_prompt text,
    public_rules_description text,
    max_trials_per_person_per_season INTEGER,
    CONSTRAINT organizations_max_trials_per_person_per_season_check CHECK ((max_trials_per_person_per_season > 0)),
    CONSTRAINT organizations_name_check CHECK ((trim(name) <> '')),
    CONSTRAINT organizations_public_email_check CHECK (((public_email IS NULL) OR (valid_public_email(public_email)))),
    CONSTRAINT organizations_public_phone_check CHECK (((public_phone IS NULL) OR (trim(public_phone) <> ''))),
    CONSTRAINT organizations_website_url_check CHECK (((website_url IS NULL) OR (valid_public_url(website_url))))
);

CREATE TABLE person_emergency_contacts (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id INTEGER NOT NULL,
    contact_person_id INTEGER NOT NULL,
    relationship_label text,
    priority INTEGER NOT NULL,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT person_emergency_contacts_check CHECK ((person_id <> contact_person_id)),
    CONSTRAINT person_emergency_contacts_priority_check CHECK ((priority > 0)),
    CONSTRAINT person_emergency_contacts_person_id_contact_person_id_key UNIQUE (person_id, contact_person_id),
    CONSTRAINT person_emergency_contacts_person_id_priority_key UNIQUE (person_id, priority),
    CONSTRAINT person_emergency_contacts_contact_person_id_fkey FOREIGN KEY (contact_person_id) REFERENCES persons(id),
    CONSTRAINT person_emergency_contacts_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id)
);

CREATE TABLE person_guardians (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    child_person_id INTEGER NOT NULL,
    guardian_person_id INTEGER NOT NULL,
    relationship_type text NOT NULL,
    is_primary_contact boolean DEFAULT false NOT NULL CHECK (is_primary_contact IN (0,1)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT person_guardians_no_self CHECK ((child_person_id <> guardian_person_id)),
    CONSTRAINT person_guardians_relationship_type_check CHECK ((relationship_type IN ('mother', 'father', 'guardian', 'other'))),
    CONSTRAINT person_guardians_unique_pair UNIQUE (child_person_id, guardian_person_id),
    CONSTRAINT person_guardians_child_person_id_fkey FOREIGN KEY (child_person_id) REFERENCES persons(id),
    CONSTRAINT person_guardians_guardian_person_id_fkey FOREIGN KEY (guardian_person_id) REFERENCES persons(id)
);

CREATE TABLE persons (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    first_name text NOT NULL,
    last_name text NOT NULL,
    birth_date date CHECK (birth_date IS NULL OR (length(birth_date)=10 AND birth_date BETWEEN '0001-01-01' AND '9999-12-31' AND date(birth_date,'+0 days') IS birth_date)),
    phone_number text,
    email text,
    address text,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    archived_at DATETIME,
    notes text,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL
);

CREATE TABLE registration_application_activities (

    application_id INTEGER NOT NULL,
    activity_id INTEGER NOT NULL,
    CONSTRAINT registration_application_activities_pkey PRIMARY KEY (application_id, activity_id),
    CONSTRAINT registration_application_activities_activity_id_fkey FOREIGN KEY (activity_id) REFERENCES activities(id),
    CONSTRAINT registration_application_activities_application_id_fkey FOREIGN KEY (application_id) REFERENCES registration_applications(id)
);

CREATE TABLE registration_application_consents (

    application_id INTEGER NOT NULL,
    consent_definition_id INTEGER NOT NULL,
    decision text NOT NULL,
    presented_at DATETIME NOT NULL,
    CONSTRAINT registration_application_consents_decision_check CHECK ((decision IN ('granted', 'refused'))),
    CONSTRAINT registration_application_consents_pkey PRIMARY KEY (application_id, consent_definition_id),
    CONSTRAINT registration_application_consents_application_id_fkey FOREIGN KEY (application_id) REFERENCES registration_applications(id),
    CONSTRAINT registration_application_consents_consent_definition_id_fkey FOREIGN KEY (consent_definition_id) REFERENCES consent_definitions(id)
);

CREATE TABLE registration_applications (
    required_activity_id INTEGER NOT NULL,

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    submission_id INTEGER NOT NULL,
    request_key BLOB NOT NULL,
    season_id INTEGER NOT NULL,
    membership_type_id INTEGER NOT NULL,
    status text DEFAULT 'awaiting_identity' NOT NULL,
    membership_id INTEGER,
    last_error_code text,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    finalized_at DATETIME,
    CONSTRAINT registration_applications_check CHECK (((status = 'membership_created') = (membership_id IS NOT NULL))),
    CONSTRAINT registration_applications_check1 CHECK (((status = 'membership_created') = (finalized_at IS NOT NULL))),
    CONSTRAINT registration_applications_check2 CHECK (((status = 'needs_review') = (last_error_code IS NOT NULL))),
    CONSTRAINT registration_applications_last_error_code_check CHECK ((last_error_code IN ('membership_already_exists', 'choices_unavailable', 'member_not_adult', 'membership_unavailable', 'guardian_identity_review', 'child_identity_review', 'guardian_confirmation_required', 'member_not_minor', 'guardian_relation_invalid'))),
    CONSTRAINT registration_applications_request_key_check CHECK ((length(request_key) = 32)),
    CONSTRAINT registration_applications_status_check CHECK ((status IN ('awaiting_identity', 'membership_created', 'needs_review', 'cancelled'))),
    CONSTRAINT registration_applications_membership_id_key UNIQUE (membership_id),
    CONSTRAINT registration_applications_request_key_key UNIQUE (request_key),
    CONSTRAINT registration_applications_submission_id_key UNIQUE (submission_id),
    CONSTRAINT registration_applications_membership_id_fkey FOREIGN KEY (membership_id) REFERENCES memberships(id),
    CONSTRAINT registration_applications_membership_type_id_fkey FOREIGN KEY (membership_type_id) REFERENCES membership_types(id),
    CONSTRAINT registration_applications_season_id_fkey FOREIGN KEY (season_id) REFERENCES seasons(id),
    CONSTRAINT registration_applications_submission_id_fkey FOREIGN KEY (submission_id) REFERENCES registration_submissions(id),
    CONSTRAINT registration_activity_required FOREIGN KEY (id,required_activity_id) REFERENCES registration_application_activities(application_id,activity_id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE registration_email_verifications (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    submission_id INTEGER NOT NULL,
    person_id INTEGER NOT NULL,
    public_reference text NOT NULL,
    code_hash BLOB NOT NULL,
    recipient_hash BLOB NOT NULL,
    expires_at DATETIME NOT NULL,
    used_at DATETIME,
    invalidated_at DATETIME,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT registration_email_verifications_check CHECK (((used_at IS NULL) OR (invalidated_at IS NULL))),
    CONSTRAINT registration_email_verifications_check1 CHECK ((expires_at > created_at)),
    CONSTRAINT registration_email_verifications_code_hash_check CHECK ((length(code_hash) = 32)),
    CONSTRAINT registration_email_verifications_public_reference_check CHECK ((length(public_reference) = 64)),
    CONSTRAINT registration_email_verifications_recipient_hash_check CHECK ((length(recipient_hash) = 32)),
    CONSTRAINT registration_email_verifications_public_reference_key UNIQUE (public_reference),
    CONSTRAINT registration_email_verifications_submission_id_person_id_fkey FOREIGN KEY (submission_id, person_id) REFERENCES registration_submission_candidates(submission_id, person_id)
);

CREATE TABLE registration_submission_candidates (

    submission_id INTEGER NOT NULL,
    person_id INTEGER NOT NULL,
    confidence text NOT NULL,
    matched_name boolean NOT NULL CHECK (matched_name IN (0,1)),
    matched_birth_date boolean NOT NULL CHECK (matched_birth_date IN (0,1)),
    matched_email boolean NOT NULL CHECK (matched_email IN (0,1)),
    matched_phone boolean NOT NULL CHECK (matched_phone IN (0,1)),
    detected_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT registration_submission_candidates_confidence_check CHECK ((confidence IN ('strong', 'possible', 'weak'))),
    CONSTRAINT registration_submission_candidates_pkey PRIMARY KEY (submission_id, person_id),
    CONSTRAINT registration_submission_candidates_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id),
    CONSTRAINT registration_submission_candidates_submission_id_fkey FOREIGN KEY (submission_id) REFERENCES registration_submissions(id)
);

CREATE TABLE registration_submissions (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status text DEFAULT 'received' NOT NULL,
    first_name text NOT NULL,
    last_name text NOT NULL,
    birth_date date CHECK (birth_date IS NULL OR (length(birth_date)=10 AND birth_date BETWEEN '0001-01-01' AND '9999-12-31' AND date(birth_date,'+0 days') IS birth_date)),
    email text,
    phone_number text,
    address text,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    resolved_person_id INTEGER,
    resolution_type text,
    resolved_at DATETIME,
    resolved_by_user_id INTEGER,
    CONSTRAINT registration_resolution_consistent CHECK ((((status = 'resolved') AND (resolved_person_id IS NOT NULL) AND (resolution_type IS NOT NULL) AND (resolved_at IS NOT NULL)) OR ((status <> 'resolved') AND (resolved_person_id IS NULL) AND (resolution_type IS NULL) AND (resolved_at IS NULL) AND (resolved_by_user_id IS NULL)))),
    CONSTRAINT registration_submissions_first_name_check CHECK ((trim(first_name) <> '')),
    CONSTRAINT registration_submissions_last_name_check CHECK ((trim(last_name) <> '')),
    CONSTRAINT registration_submissions_resolution_type_check CHECK ((resolution_type IN ('existing_person', 'new_person'))),
    CONSTRAINT registration_submissions_status_check CHECK ((status IN ('received', 'awaiting_identity_review', 'awaiting_email_verification', 'resolved', 'cancelled'))),
    CONSTRAINT registration_submissions_resolved_by_user_id_fkey FOREIGN KEY (resolved_by_user_id) REFERENCES users(id),
    CONSTRAINT registration_submissions_resolved_person_id_fkey FOREIGN KEY (resolved_person_id) REFERENCES persons(id)
);

CREATE TABLE registration_verification_outbox (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    submission_id INTEGER NOT NULL,
    status text DEFAULT 'pending' NOT NULL,
    attempt_count INTEGER DEFAULT 0 NOT NULL,
    available_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    lease_until DATETIME,
    lease_version bigint DEFAULT 0 NOT NULL,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    sent_at DATETIME,
    finished_at DATETIME,
    last_error_code text,
    recipient_hash BLOB NOT NULL,
    CONSTRAINT registration_verification_outbox_attempt_count_check CHECK (((attempt_count >= 0) AND (attempt_count <= 3))),
    CONSTRAINT registration_verification_outbox_check CHECK (((status = 'processing') = (lease_until IS NOT NULL))),
    CONSTRAINT registration_verification_outbox_check1 CHECK (((status IN ('sent', 'dead', 'cancelled')) = (finished_at IS NOT NULL))),
    CONSTRAINT registration_verification_outbox_check2 CHECK (((status = 'sent') = (sent_at IS NOT NULL))),
    CONSTRAINT registration_verification_outbox_last_error_code_check CHECK ((last_error_code IN ('smtp_failed', 'ineligible', 'attempts_exhausted', 'mailer_disabled', 'submission_closed'))),
    CONSTRAINT registration_verification_outbox_lease_version_check CHECK ((lease_version >= 0)),
    CONSTRAINT registration_verification_outbox_recipient_hash_check CHECK ((length(recipient_hash) = 32)),
    CONSTRAINT registration_verification_outbox_status_check CHECK ((status IN ('pending', 'processing', 'sent', 'dead', 'cancelled'))),
    CONSTRAINT registration_verification_outbox_submission_id_fkey FOREIGN KEY (submission_id) REFERENCES registration_submissions(id)
);

CREATE TABLE roles (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name text NOT NULL,
    CONSTRAINT roles_name_key UNIQUE (name)
);

CREATE TABLE seasons (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name text NOT NULL,
    starts_at date NOT NULL CHECK (starts_at IS NULL OR (length(starts_at)=10 AND starts_at BETWEEN '0001-01-01' AND '9999-12-31' AND date(starts_at,'+0 days') IS starts_at)),
    ends_at date NOT NULL CHECK (ends_at IS NULL OR (length(ends_at)=10 AND ends_at BETWEEN '0001-01-01' AND '9999-12-31' AND date(ends_at,'+0 days') IS ends_at)),
    is_active boolean DEFAULT true NOT NULL CHECK (is_active IN (0,1)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT seasons_name_key UNIQUE (name)
);

CREATE TABLE trial_registrations (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id INTEGER NOT NULL,
    activity_id INTEGER NOT NULL,
    trial_date date NOT NULL CHECK (trial_date IS NULL OR (length(trial_date)=10 AND trial_date BETWEEN '0001-01-01' AND '9999-12-31' AND date(trial_date,'+0 days') IS trial_date)),
    status text NOT NULL,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    notes text,
    group_id INTEGER,
    group_slot_id INTEGER,
    revision INTEGER DEFAULT 0 NOT NULL,
    CONSTRAINT trial_registrations_status_check CHECK ((status IN ('registered', 'attended', 'cancelled', 'no_show'))),
    CONSTRAINT trial_slot_requires_group CHECK (((group_slot_id IS NULL) OR (group_id IS NOT NULL))),
    CONSTRAINT trial_group_activity_fk FOREIGN KEY (group_id, activity_id) REFERENCES groups(id, activity_id),
    CONSTRAINT trial_registrations_activity_id_fkey FOREIGN KEY (activity_id) REFERENCES activities(id),
    CONSTRAINT trial_registrations_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id),
    CONSTRAINT trial_slot_group_fk FOREIGN KEY (group_slot_id, group_id) REFERENCES group_slots(id, group_id)
);

CREATE TABLE user_activation_codes (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    code_hash BLOB NOT NULL,
    expires_at DATETIME NOT NULL,
    used_at DATETIME,
    invalidated_at DATETIME,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT user_activation_codes_check CHECK ((expires_at > created_at)),
    CONSTRAINT user_activation_codes_code_hash_check CHECK ((length(code_hash) = 32)),
    CONSTRAINT user_activation_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE user_email_change_requests (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    person_id INTEGER NOT NULL,
    new_email text NOT NULL,
    new_email_normalized text NOT NULL,
    new_email_hash BLOB NOT NULL,
    code_hash BLOB NOT NULL,
    expires_at DATETIME NOT NULL,
    used_at DATETIME,
    invalidated_at DATETIME,
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    CONSTRAINT user_email_change_requests_code_hash_check CHECK ((length(code_hash) = 32)),
    CONSTRAINT user_email_change_requests_new_email_hash_check CHECK ((length(new_email_hash) = 32)),
    CONSTRAINT user_email_change_requests_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id),
    CONSTRAINT user_email_change_requests_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE user_roles (

    user_id INTEGER NOT NULL,
    role_id INTEGER NOT NULL,
    CONSTRAINT user_roles_pkey PRIMARY KEY (user_id, role_id),
    CONSTRAINT user_roles_role_id_fkey FOREIGN KEY (role_id) REFERENCES roles(id),
    CONSTRAINT user_roles_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE users (

    id INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id INTEGER NOT NULL,
    login_email text,
    password_hash text,
    is_active boolean DEFAULT true NOT NULL CHECK (is_active IN (0,1)),
    created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')) NOT NULL,
    username text NOT NULL,
    activated_at DATETIME,
    CONSTRAINT users_username_nonempty CHECK ((trim(username) <> '')),
    CONSTRAINT users_person_id_key UNIQUE (person_id),
    CONSTRAINT users_username_key UNIQUE (username),
    CONSTRAINT users_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id)
);

CREATE INDEX administrative_events_membership_trial_group_idx ON administrative_events (resource_id, action) WHERE ((resource_type = 'membership') AND (action IN ('membership_trial_group_assigned', 'membership_trial_group_skipped')));
CREATE UNIQUE INDEX consent_definitions_one_active_code ON consent_definitions (code) WHERE is_active;
CREATE INDEX group_slots_group_season_idx ON group_slots (group_id, season_id);
CREATE INDEX group_slots_location_idx ON group_slots (location_id);
CREATE INDEX group_slots_season_id_idx ON group_slots (season_id);
CREATE INDEX guardian_access_guardian ON guardian_access_grants (guardian_person_id, child_person_id) WHERE (revoked_at IS NULL);
CREATE UNIQUE INDEX guardian_access_one_active_pair ON guardian_access_grants (child_person_id, guardian_person_id) WHERE (revoked_at IS NULL);
CREATE INDEX membership_consents_definition_idx ON membership_consents (consent_definition_id);
CREATE INDEX membership_consents_given_by_idx ON membership_consents (given_by_person_id);
CREATE INDEX membership_consents_history_idx ON membership_consents (membership_id, consent_definition_id, recorded_at DESC, id DESC);
CREATE INDEX membership_groups_group_id_idx ON membership_groups (group_id);
CREATE INDEX membership_groups_membership_id_idx ON membership_groups (membership_id);
CREATE UNIQUE INDEX membership_groups_unique_open_assignment ON membership_groups (membership_id, group_id) WHERE (left_at IS NULL);
CREATE UNIQUE INDEX memberships_source_trial_unique ON memberships (source_trial_id) WHERE (source_trial_id IS NOT NULL);
CREATE UNIQUE INDEX organizations_one_active ON organizations ((true)) WHERE is_active;
CREATE INDEX person_emergency_contacts_contact_idx ON person_emergency_contacts (contact_person_id);
CREATE INDEX person_guardians_guardian_person_id_idx ON person_guardians (guardian_person_id);
CREATE UNIQUE INDEX person_guardians_one_primary_contact ON person_guardians (child_person_id) WHERE is_primary_contact;
CREATE INDEX registration_applications_review ON registration_applications (status, id);
CREATE INDEX registration_email_expiration ON registration_email_verifications (expires_at) WHERE ((used_at IS NULL) AND (invalidated_at IS NULL));
CREATE UNIQUE INDEX registration_email_one_open ON registration_email_verifications (submission_id) WHERE ((used_at IS NULL) AND (invalidated_at IS NULL));
CREATE INDEX registration_outbox_lease ON registration_verification_outbox (lease_until, id) WHERE (status = 'processing');
CREATE UNIQUE INDEX registration_outbox_one_active ON registration_verification_outbox (submission_id) WHERE (status IN ('pending', 'processing'));
CREATE INDEX registration_outbox_pending ON registration_verification_outbox (available_at, id) WHERE (status = 'pending');
CREATE INDEX registration_outbox_recipient ON registration_verification_outbox (recipient_hash, created_at);
CREATE INDEX registration_submission_candidates_person_idx ON registration_submission_candidates (person_id);
CREATE INDEX registration_submissions_queue_idx ON registration_submissions (status, created_at, id);
CREATE INDEX trial_registrations_date_idx ON trial_registrations (trial_date);
CREATE INDEX trial_registrations_group_idx ON trial_registrations (group_id);
CREATE INDEX trial_registrations_person_date_idx ON trial_registrations (person_id, trial_date);
CREATE INDEX trial_registrations_slot_idx ON trial_registrations (group_slot_id);
CREATE UNIQUE INDEX user_activation_codes_one_current ON user_activation_codes (user_id) WHERE ((used_at IS NULL) AND (invalidated_at IS NULL));
CREATE INDEX user_email_change_budget ON user_email_change_requests (user_id, created_at);
CREATE UNIQUE INDEX user_email_change_one_active ON user_email_change_requests (user_id) WHERE ((used_at IS NULL) AND (invalidated_at IS NULL));


-- +goose StatementBegin
CREATE TRIGGER membership_consents_no_update BEFORE UPDATE ON membership_consents
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is append only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER membership_consents_no_delete BEFORE DELETE ON membership_consents
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is append only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_submission_candidates_no_update BEFORE UPDATE ON registration_submission_candidates
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is append only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_submission_candidates_no_delete BEFORE DELETE ON registration_submission_candidates
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is append only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER guardian_identity_claim_candidates_no_update BEFORE UPDATE ON guardian_identity_claim_candidates
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is append only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER guardian_identity_claim_candidates_no_delete BEFORE DELETE ON guardian_identity_claim_candidates
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is append only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_application_activities_no_update BEFORE UPDATE ON registration_application_activities
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is append only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_application_activities_no_delete BEFORE DELETE ON registration_application_activities
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is append only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_application_consents_no_update BEFORE UPDATE ON registration_application_consents
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is append only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_application_consents_no_delete BEFORE DELETE ON registration_application_consents
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is append only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER consent_definitions_no_delete BEFORE DELETE ON consent_definitions
BEGIN
 SELECT RAISE(ABORT, 'audit history is retained');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER consent_definitions_immutable BEFORE UPDATE ON consent_definitions
WHEN NEW.id IS NOT OLD.id OR NEW.code IS NOT OLD.code OR NEW.version IS NOT OLD.version OR NEW.title IS NOT OLD.title OR NEW.description IS NOT OLD.description OR NEW.created_at IS NOT OLD.created_at
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_submissions_no_delete BEFORE DELETE ON registration_submissions
BEGIN
 SELECT RAISE(ABORT, 'audit history is retained');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_submissions_immutable BEFORE UPDATE ON registration_submissions
WHEN (OLD.status IN ('resolved','cancelled')) OR NEW.id IS NOT OLD.id OR NEW.first_name IS NOT OLD.first_name OR NEW.last_name IS NOT OLD.last_name OR NEW.birth_date IS NOT OLD.birth_date OR NEW.email IS NOT OLD.email OR NEW.phone_number IS NOT OLD.phone_number OR NEW.address IS NOT OLD.address OR NEW.created_at IS NOT OLD.created_at
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER guardian_identity_claims_no_delete BEFORE DELETE ON guardian_identity_claims
BEGIN
 SELECT RAISE(ABORT, 'audit history is retained');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER guardian_identity_claims_immutable BEFORE UPDATE ON guardian_identity_claims
WHEN (OLD.status IN ('resolved','cancelled')) OR NEW.id IS NOT OLD.id OR NEW.first_name IS NOT OLD.first_name OR NEW.last_name IS NOT OLD.last_name OR NEW.birth_date IS NOT OLD.birth_date OR NEW.email IS NOT OLD.email OR NEW.phone_number IS NOT OLD.phone_number OR NEW.address IS NOT OLD.address OR NEW.created_at IS NOT OLD.created_at
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_applications_no_delete BEFORE DELETE ON registration_applications
BEGIN
 SELECT RAISE(ABORT, 'audit history is retained');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_applications_immutable BEFORE UPDATE ON registration_applications
WHEN (OLD.status IN ('membership_created','cancelled')) OR NEW.id IS NOT OLD.id OR NEW.submission_id IS NOT OLD.submission_id OR NEW.request_key IS NOT OLD.request_key OR NEW.season_id IS NOT OLD.season_id OR NEW.membership_type_id IS NOT OLD.membership_type_id OR NEW.created_at IS NOT OLD.created_at OR NEW.required_activity_id IS NOT OLD.required_activity_id
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_email_verifications_no_delete BEFORE DELETE ON registration_email_verifications
BEGIN
 SELECT RAISE(ABORT, 'audit history is retained');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_email_verifications_immutable BEFORE UPDATE ON registration_email_verifications
WHEN (OLD.used_at IS NOT NULL OR OLD.invalidated_at IS NOT NULL) OR NEW.id IS NOT OLD.id OR NEW.submission_id IS NOT OLD.submission_id OR NEW.person_id IS NOT OLD.person_id OR NEW.public_reference IS NOT OLD.public_reference OR NEW.code_hash IS NOT OLD.code_hash OR NEW.recipient_hash IS NOT OLD.recipient_hash OR NEW.created_at IS NOT OLD.created_at OR NEW.expires_at IS NOT OLD.expires_at
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_verification_outbox_no_delete BEFORE DELETE ON registration_verification_outbox
BEGIN
 SELECT RAISE(ABORT, 'audit history is retained');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_verification_outbox_immutable BEFORE UPDATE ON registration_verification_outbox
WHEN (OLD.status IN ('sent','dead','cancelled')) OR NEW.id IS NOT OLD.id OR NEW.submission_id IS NOT OLD.submission_id OR NEW.recipient_hash IS NOT OLD.recipient_hash OR NEW.created_at IS NOT OLD.created_at
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER child_registration_applications_no_delete BEFORE DELETE ON child_registration_applications
BEGIN
 SELECT RAISE(ABORT, 'audit history is retained');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER child_registration_applications_immutable BEFORE UPDATE ON child_registration_applications
WHEN (OLD.guardian_confirmed_at IS NOT NULL) OR NEW.application_id IS NOT OLD.application_id OR NEW.guardian_claim_id IS NOT OLD.guardian_claim_id OR NEW.relationship_type IS NOT OLD.relationship_type OR NEW.emergency_contact_requested IS NOT OLD.emergency_contact_requested OR NEW.created_at IS NOT OLD.created_at
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER guardian_access_grants_no_delete BEFORE DELETE ON guardian_access_grants
BEGIN
 SELECT RAISE(ABORT, 'audit history is retained');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER guardian_access_grants_immutable BEFORE UPDATE ON guardian_access_grants
WHEN (OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL) OR NEW.id IS NOT OLD.id OR NEW.child_person_id IS NOT OLD.child_person_id OR NEW.guardian_person_id IS NOT OLD.guardian_person_id OR NEW.granted_at IS NOT OLD.granted_at OR NEW.granted_by_user_id IS NOT OLD.granted_by_user_id OR NEW.created_at IS NOT OLD.created_at
BEGIN
 SELECT RAISE(ABORT, 'audit evidence is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER guardian_access_requires_relation BEFORE INSERT ON guardian_access_grants
WHEN NOT EXISTS(SELECT 1 FROM person_guardians WHERE child_person_id=NEW.child_person_id AND guardian_person_id=NEW.guardian_person_id)
BEGIN
 SELECT RAISE(ABORT, 'guardian relationship required');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER guardian_relation_access_delete BEFORE DELETE ON person_guardians
WHEN EXISTS(SELECT 1 FROM guardian_access_grants WHERE child_person_id=OLD.child_person_id AND guardian_person_id=OLD.guardian_person_id AND revoked_at IS NULL)
BEGIN
 SELECT RAISE(ABORT, 'revoke guardian access before removing relationship');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER guardian_relation_access_update BEFORE UPDATE ON person_guardians
WHEN EXISTS(SELECT 1 FROM guardian_access_grants WHERE child_person_id=OLD.child_person_id AND guardian_person_id=OLD.guardian_person_id AND revoked_at IS NULL) AND (NEW.child_person_id IS NOT OLD.child_person_id OR NEW.guardian_person_id IS NOT OLD.guardian_person_id)
BEGIN
 SELECT RAISE(ABORT, 'revoke guardian access before removing relationship');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER registration_outbox_closed AFTER UPDATE ON registration_submissions
WHEN NEW.status IN ('resolved','cancelled') AND OLD.status IS NOT NEW.status
BEGIN
 UPDATE registration_verification_outbox SET status='cancelled',lease_until=NULL,
 finished_at=strftime('%Y-%m-%d %H:%M:%f','now'),updated_at=strftime('%Y-%m-%d %H:%M:%f','now'),last_error_code='submission_closed'
 WHERE submission_id=NEW.id AND status IN ('pending','processing');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER membership_consent_requirements_no_update BEFORE UPDATE ON membership_consent_requirements
BEGIN
 SELECT RAISE(ABORT, 'membership consent requirements are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER membership_consent_requirements_no_delete BEFORE DELETE ON membership_consent_requirements
BEGIN
 SELECT RAISE(ABORT, 'membership consent requirements are immutable');
END;
-- +goose StatementEnd

INSERT INTO installation_setup(id) VALUES(TRUE);
INSERT INTO roles(name) VALUES ('president'),('secretary'),('treasurer'),('coach');

-- +goose Down
CREATE TABLE baseline_rollback_guard (empty INTEGER CHECK(empty=0));
INSERT INTO baseline_rollback_guard SELECT count(*) FROM membership_consents;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM registration_submissions;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM registration_applications;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM registration_verification_outbox;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM guardian_access_grants;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM guardian_identity_claims;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM child_registration_applications;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM administrative_events;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM account_security_events;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM activities;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM consent_definitions;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM group_slots;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM groups;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM guardian_identity_claim_candidates;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM locations;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM membership_activities;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM membership_consent_requirements;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM membership_groups;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM membership_types;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM memberships;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM organization_links;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM organization_public_images;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM organizations;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM person_emergency_contacts;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM person_guardians;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM persons;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM registration_application_activities;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM registration_application_consents;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM registration_email_verifications;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM registration_submission_candidates;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM seasons;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM trial_registrations;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM user_activation_codes;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM user_email_change_requests;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM user_roles;
INSERT INTO baseline_rollback_guard SELECT count(*) FROM users;
DROP TABLE baseline_rollback_guard;
DROP TABLE child_registration_applications;
DROP TABLE guardian_identity_claim_candidates;
DROP TABLE guardian_identity_claims;
DROP TABLE registration_application_consents;
DROP TABLE registration_applications;
DROP TABLE registration_application_activities;
DROP TABLE registration_email_verifications;
DROP TABLE registration_verification_outbox;
DROP TABLE registration_submission_candidates;
DROP TABLE registration_submissions;
DROP TABLE guardian_access_grants;
DROP TABLE account_security_events;
DROP TABLE administrative_events;
DROP TABLE installation_setup;
DROP TABLE user_email_change_requests;
DROP TABLE user_activation_codes;
DROP TABLE user_roles;
DROP TABLE users;
DROP TABLE membership_consents;
DROP TABLE membership_consent_requirements;
DROP TABLE membership_groups;
DROP TABLE membership_activities;
DROP TABLE memberships;
DROP TABLE trial_registrations;
DROP TABLE person_emergency_contacts;
DROP TABLE person_guardians;
DROP TABLE persons;
DROP TABLE group_slots;
DROP TABLE groups;
DROP TABLE activities;
DROP TABLE seasons;
DROP TABLE membership_types;
DROP TABLE consent_definitions;
DROP TABLE organization_public_images;
DROP TABLE organization_links;
DROP TABLE locations;
DROP TABLE organizations;
DROP TABLE roles;
