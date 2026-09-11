-- name: GetMembership :one
SELECT * FROM memberships WHERE id = $1;

-- name: GetMembershipDetails :one
SELECT sqlc.embed(m), sqlc.embed(p), sqlc.embed(s), sqlc.embed(t),
       u.id AS user_id, u.username, u.is_active AS user_is_active, u.activated_at,
       approver.username AS approver_username
FROM memberships m
JOIN persons p ON p.id = m.person_id
JOIN seasons s ON s.id = m.season_id
JOIN membership_types t ON t.id = m.membership_type_id
LEFT JOIN users u ON u.person_id = p.id
LEFT JOIN users approver ON approver.id = m.approved_by_user_id
WHERE m.id = $1;

-- name: ListMembershipActivities :many
SELECT a.* FROM activities a JOIN membership_activities ma ON ma.activity_id = a.id
WHERE ma.membership_id = $1 ORDER BY a.name, a.id;

-- name: ListMembershipConsentRequirements :many
SELECT r.presented_at, d.*, c.decision, c.given_by_person_id, c.recorded_at
FROM membership_consent_requirements r
JOIN consent_definitions d ON d.id = r.consent_definition_id
LEFT JOIN membership_consents c ON c.id = (
 SELECT mc.id FROM membership_consents mc
 WHERE mc.membership_id = r.membership_id AND mc.consent_definition_id = r.consent_definition_id
 ORDER BY mc.recorded_at DESC, mc.id DESC LIMIT 1
)
WHERE r.membership_id = $1 ORDER BY d.code, d.version;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: GetUserByPerson :one
SELECT * FROM users WHERE person_id = $1;
