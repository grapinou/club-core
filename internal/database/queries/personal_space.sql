-- Safe read projections only. Resource ownership is supplied by personalspace
-- after deriving the caller's Person or checking GuardianAccess.
-- name: GetPersonalAccount :one
SELECT p.id AS person_id, p.first_name, p.last_name, p.email, p.phone_number, p.address, p.birth_date, u.username,
 CAST(EXISTS(SELECT 1 FROM identity_correction_requests c WHERE c.person_id=p.id AND c.status='pending') AS BOOLEAN) AS identity_correction_pending
FROM users u JOIN persons p ON p.id=u.person_id
WHERE u.id=sqlc.arg(id) AND u.is_active AND u.activated_at IS NOT NULL
 AND u.password_hash IS NOT NULL AND p.archived_at IS NULL;

-- name: GetPersonalChild :one
SELECT p.first_name,p.last_name,p.birth_date,
 EXISTS(SELECT 1 FROM person_emergency_contacts e WHERE e.person_id=p.id) AS has_emergency,
 EXISTS(SELECT 1 FROM person_emergency_contacts e WHERE e.person_id=p.id
 AND e.contact_person_id=sqlc.arg(viewer_person_id)) AS viewer_is_emergency
FROM persons p WHERE p.id=sqlc.arg(child_person_id) AND p.archived_at IS NULL;

-- name: ListPersonalMembershipSummaries :many
SELECT m.id,m.person_id,m.season_id,m.status,m.requested_at,
 CAST((SELECT json_group_array(value) FROM (SELECT g.name AS value FROM membership_groups mg JOIN groups g ON g.id=mg.group_id
 WHERE mg.membership_id=m.id AND g.is_active AND mg.joined_at<=sqlc.arg(today) AND (mg.left_at IS NULL OR mg.left_at>sqlc.arg(today)) ORDER BY g.name,g.id)) AS JSON_TEXT_STRINGS) AS group_names,
 CAST((s.starts_at<=sqlc.arg(today) AND s.ends_at>=sqlc.arg(today)) AS BOOLEAN) AS is_current,
 s.name AS season_name,t.name AS membership_type_name,
 CAST((SELECT json_group_array(value) FROM (SELECT a.name AS value FROM activities a JOIN membership_activities ma ON ma.activity_id=a.id
 WHERE ma.membership_id=m.id ORDER BY a.name,a.id)) AS JSON_TEXT_STRINGS) AS activities
FROM memberships m JOIN seasons s ON s.id=m.season_id JOIN membership_types t ON t.id=m.membership_type_id
WHERE m.person_id IN (sqlc.slice(ids)) ORDER BY is_current DESC,s.starts_at DESC,m.id DESC;

-- name: GetPersonalMembership :one
SELECT m.id,m.status,m.requested_at,m.joined_at,s.name AS season_name,t.name AS membership_type_name,
 CAST((SELECT json_group_array(value) FROM (SELECT a.name AS value FROM activities a JOIN membership_activities ma ON ma.activity_id=a.id
 WHERE ma.membership_id=m.id ORDER BY a.name,a.id)) AS JSON_TEXT_STRINGS) AS activities
FROM memberships m JOIN seasons s ON s.id=m.season_id JOIN membership_types t ON t.id=m.membership_type_id
WHERE m.id=sqlc.arg(membership_id) AND m.person_id=sqlc.arg(person_id);

-- name: ListPersonalConsents :many
SELECT d.id,d.is_active,d.title,d.version,d.description,c.decision,c.recorded_at, CAST(COALESCE(c.given_by_person_id=sqlc.arg(viewer_person_id),false) AS BOOLEAN) AS given_by_viewer
FROM membership_consent_requirements r JOIN consent_definitions d ON d.id=r.consent_definition_id
LEFT JOIN membership_consents c ON c.id=(SELECT mc.id FROM membership_consents mc
 WHERE mc.membership_id=r.membership_id AND mc.consent_definition_id=r.consent_definition_id
 ORDER BY mc.recorded_at DESC,mc.id DESC LIMIT 1)
WHERE r.membership_id=sqlc.arg(membership_id) ORDER BY d.code,d.version;

-- Current assignment [joined_at,left_at); current slot validity is inclusive.
-- name: ListPersonalGroups :many
SELECT g.name AS group_name,a.name AS activity_name,gs.weekday,
 CAST(COALESCE(CAST(substr(gs.start_time,1,5) AS TEXT),'') AS TEXT) AS start_time,
 CAST(COALESCE(CAST(substr(gs.end_time,1,5) AS TEXT),'') AS TEXT) AS end_time,COALESCE((SELECT l.name FROM locations l WHERE l.id=gs.location_id),gs.location) AS location
FROM membership_groups mg JOIN memberships m ON m.id=mg.membership_id
JOIN groups g ON g.id=mg.group_id AND g.is_active
JOIN activities a ON a.id=g.activity_id
LEFT JOIN group_slots gs ON gs.group_id=g.id AND gs.season_id=m.season_id
 AND gs.is_active AND gs.valid_from<=sqlc.arg(today)
 AND (gs.valid_until IS NULL OR gs.valid_until>=sqlc.arg(today))
WHERE mg.membership_id=sqlc.arg(membership_id) AND mg.joined_at<=sqlc.arg(today)
 AND (mg.left_at IS NULL OR mg.left_at>sqlc.arg(today))
ORDER BY g.name,g.id,gs.weekday,gs.start_time,gs.id;

-- Only authenticated family submissions made by this account. Declared child
-- data stays in staging; matching and resolved identities are never projected.
-- name: ListPendingFamilyRequests :many
SELECT s.first_name,s.last_name,s.created_at
FROM registration_applications a
JOIN registration_submissions s ON s.id=a.submission_id
JOIN child_registration_applications c ON c.application_id=a.id
JOIN guardian_identity_claims g ON g.id=c.guardian_claim_id
WHERE g.resolved_by_user_id=sqlc.arg(viewer_user_id)
 AND g.resolved_person_id=sqlc.arg(viewer_person_id)
 AND a.status IN ('awaiting_identity','needs_review')
 AND (s.resolved_person_id IS NULL OR NOT(s.resolved_person_id IN (sqlc.slice(managed_children))))
ORDER BY s.created_at DESC,s.id DESC;

-- name: HasOwnPersonalContext :one
SELECT EXISTS(SELECT 1 FROM memberships WHERE person_id=p.id)
 OR EXISTS(SELECT 1 FROM registration_applications a
 JOIN registration_submissions s ON s.id=a.submission_id
 JOIN child_registration_applications c ON c.application_id=a.id
 JOIN guardian_identity_claims g ON g.id=c.guardian_claim_id
 WHERE g.resolved_by_user_id=u.id AND g.resolved_person_id=p.id
 AND a.status IN ('awaiting_identity','needs_review')) AS has_context
FROM users u JOIN persons p ON p.id=u.person_id
WHERE u.id=sqlc.arg(id) AND u.is_active AND u.activated_at IS NOT NULL AND u.password_hash IS NOT NULL AND p.archived_at IS NULL;

-- name: ListPersonalEmergencyContacts :many
SELECT e.person_id,e.priority,e.relationship_label,p.first_name,p.last_name,p.phone_number
FROM person_emergency_contacts e JOIN persons p ON p.id=e.contact_person_id
WHERE e.person_id IN (sqlc.slice(ids)) ORDER BY e.person_id,e.priority;
