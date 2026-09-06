-- name: AssignMembershipGroup :one
INSERT INTO membership_groups (membership_id, group_id, joined_at)
VALUES ($1, $2, $3) RETURNING *;

-- name: CloseMembershipGroup :one
UPDATE membership_groups SET left_at = sqlc.arg(left_at)::date
WHERE id = $1 RETURNING *;

-- name: ListMembershipGroupHistory :many
SELECT mg.*, g.name AS group_name, g.is_active AS group_is_active,
       g.activity_id, a.name AS activity_name
FROM membership_groups mg
JOIN groups g ON g.id = mg.group_id
JOIN activities a ON a.id = g.activity_id
WHERE mg.membership_id = $1
ORDER BY mg.joined_at, mg.id;

-- name: ListCurrentMembershipGroups :many
SELECT mg.*, g.name AS group_name, g.is_active AS group_is_active,
       g.activity_id, a.name AS activity_name
FROM membership_groups mg
JOIN groups g ON g.id = mg.group_id
JOIN activities a ON a.id = g.activity_id
WHERE mg.membership_id = $1
  AND mg.joined_at <= CURRENT_DATE
  AND (mg.left_at IS NULL OR mg.left_at > CURRENT_DATE)
ORDER BY g.name, mg.id;

-- name: ListCurrentGroupMembers :many
SELECT mg.id AS membership_group_id, m.id AS membership_id, m.season_id,
       p.id AS person_id, p.first_name, p.last_name, p.birth_date, mg.joined_at
FROM membership_groups mg
JOIN memberships m ON m.id = mg.membership_id
JOIN persons p ON p.id = m.person_id
WHERE mg.group_id = $1 AND m.season_id = $2
  AND m.status IN ('pending', 'active')
  AND mg.joined_at <= CURRENT_DATE
  AND (mg.left_at IS NULL OR mg.left_at > CURRENT_DATE)
ORDER BY p.last_name, p.first_name, mg.id;
