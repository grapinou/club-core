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
SELECT r.presented_at, d.*, c.decision, c.given_by_person_id, c.recorded_at,
 giver.first_name AS giver_first_name, giver.last_name AS giver_last_name
FROM membership_consent_requirements r
JOIN consent_definitions d ON d.id = r.consent_definition_id
LEFT JOIN membership_consents c ON c.id = (
 SELECT mc.id FROM membership_consents mc
 WHERE mc.membership_id = r.membership_id AND mc.consent_definition_id = r.consent_definition_id
 ORDER BY mc.recorded_at DESC, mc.id DESC LIMIT 1
)
LEFT JOIN persons giver ON giver.id=c.given_by_person_id
WHERE r.membership_id = $1 ORDER BY d.code, d.version;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: GetUserByPerson :one
SELECT * FROM users WHERE person_id = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: ListAdministrativeMemberships :many
SELECT sqlc.embed(m), p.first_name, p.last_name, s.name AS season_name,
       t.name AS membership_type_name,
       u.id AS user_id, u.username, u.is_active AS user_is_active, u.activated_at
FROM memberships m
JOIN persons p ON p.id=m.person_id
JOIN seasons s ON s.id=m.season_id
JOIN membership_types t ON t.id=m.membership_type_id
LEFT JOIN users u ON u.person_id=m.person_id
ORDER BY (m.status='pending') DESC,
 CASE WHEN m.status='pending' THEN m.requested_at END ASC,
 CASE WHEN m.status<>'pending' THEN m.requested_at END DESC, m.id;

-- Facts only: the shared Go evaluator owns the completeness policy.
-- name: ListMembershipCompletenessFacts :many
SELECT m.id, p.birth_date,
 EXISTS(SELECT 1 FROM membership_activities ma WHERE ma.membership_id=m.id) AS has_activity,
 EXISTS(SELECT 1 FROM person_guardians g WHERE g.child_person_id=p.id) AS has_guardian,
 EXISTS(SELECT 1 FROM person_emergency_contacts e WHERE e.person_id=p.id) AS has_emergency,
 ARRAY(
  SELECT r.consent_definition_id FROM membership_consent_requirements r
  WHERE r.membership_id=m.id AND NOT EXISTS (
   SELECT 1 FROM membership_consents c
   WHERE c.id=(
    SELECT initial.id FROM membership_consents initial
    WHERE initial.membership_id=r.membership_id AND initial.consent_definition_id=r.consent_definition_id
    ORDER BY initial.recorded_at,initial.id LIMIT 1
   ) AND c.decision IN ('granted','refused')
  ) ORDER BY r.consent_definition_id
 )::integer[] AS missing_consent_ids
FROM memberships m JOIN persons p ON p.id=m.person_id
WHERE m.id=ANY($1::integer[]);

-- name: GetMembershipIDForUser :one
SELECT m.id FROM memberships m JOIN users u ON u.person_id=m.person_id
WHERE m.id=sqlc.arg(membership_id) AND u.id=sqlc.arg(user_id);
