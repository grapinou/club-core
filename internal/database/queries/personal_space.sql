-- Safe read projections only. Resource ownership is supplied by personalspace
-- after deriving the caller's Person or checking GuardianAccess.
-- name: GetPersonalAccount :one
SELECT p.id AS person_id, p.first_name, p.last_name, p.email, p.phone_number, u.username
FROM users u JOIN persons p ON p.id=u.person_id
WHERE u.id=$1 AND u.is_active AND u.activated_at IS NOT NULL
 AND u.password_hash IS NOT NULL AND p.archived_at IS NULL;

-- name: GetPersonalChild :one
SELECT p.first_name,p.last_name,p.birth_date,
 EXISTS(SELECT 1 FROM person_emergency_contacts e WHERE e.person_id=p.id) AS has_emergency,
 EXISTS(SELECT 1 FROM person_emergency_contacts e WHERE e.person_id=p.id
 AND e.contact_person_id=sqlc.arg(viewer_person_id)) AS viewer_is_emergency
FROM persons p WHERE p.id=sqlc.arg(child_person_id) AND p.archived_at IS NULL;

-- name: ListPersonalMembershipSummaries :many
SELECT m.id,m.person_id,m.status,s.name AS season_name,t.name AS membership_type_name,
 ARRAY(SELECT a.name FROM activities a JOIN membership_activities ma ON ma.activity_id=a.id
 WHERE ma.membership_id=m.id ORDER BY a.name,a.id)::text[] AS activities
FROM memberships m JOIN seasons s ON s.id=m.season_id JOIN membership_types t ON t.id=m.membership_type_id
WHERE m.person_id=ANY($1::integer[]) ORDER BY s.starts_at DESC,m.id DESC;

-- name: GetPersonalMembership :one
SELECT m.id,m.status,m.requested_at,m.joined_at,s.name AS season_name,t.name AS membership_type_name,
 ARRAY(SELECT a.name FROM activities a JOIN membership_activities ma ON ma.activity_id=a.id
 WHERE ma.membership_id=m.id ORDER BY a.name,a.id)::text[] AS activities
FROM memberships m JOIN seasons s ON s.id=m.season_id JOIN membership_types t ON t.id=m.membership_type_id
WHERE m.id=sqlc.arg(membership_id) AND m.person_id=sqlc.arg(person_id);

-- name: ListPersonalConsents :many
SELECT d.title,d.version,d.description,c.decision,c.recorded_at,
 COALESCE(c.given_by_person_id=sqlc.arg(viewer_person_id),false)::boolean AS given_by_viewer
FROM membership_consent_requirements r JOIN consent_definitions d ON d.id=r.consent_definition_id
LEFT JOIN membership_consents c ON c.id=(SELECT mc.id FROM membership_consents mc
 WHERE mc.membership_id=r.membership_id AND mc.consent_definition_id=r.consent_definition_id
 ORDER BY mc.recorded_at DESC,mc.id DESC LIMIT 1)
WHERE r.membership_id=sqlc.arg(membership_id) ORDER BY d.code,d.version;

-- Current assignment [joined_at,left_at); current slot validity is inclusive.
-- name: ListPersonalGroups :many
SELECT g.name AS group_name,a.name AS activity_name,gs.weekday,
 COALESCE(to_char(gs.start_time,'HH24:MI'),'')::text AS start_time,
 COALESCE(to_char(gs.end_time,'HH24:MI'),'')::text AS end_time,gs.location
FROM membership_groups mg JOIN memberships m ON m.id=mg.membership_id
JOIN groups g ON g.id=mg.group_id AND g.is_active
JOIN activities a ON a.id=g.activity_id
LEFT JOIN group_slots gs ON gs.group_id=g.id AND gs.season_id=m.season_id
 AND gs.is_active AND gs.valid_from<=sqlc.arg(today)::date
 AND (gs.valid_until IS NULL OR gs.valid_until>=sqlc.arg(today)::date)
WHERE mg.membership_id=sqlc.arg(membership_id) AND mg.joined_at<=sqlc.arg(today)::date
 AND (mg.left_at IS NULL OR mg.left_at>sqlc.arg(today)::date)
ORDER BY g.name,g.id,gs.weekday,gs.start_time,gs.id;
