-- name: CreateGroupSlot :one
INSERT INTO group_slots (group_id, season_id, weekday, start_time, end_time, location, valid_from, valid_until, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING *;

-- name: UpdateGroupSlot :one
UPDATE group_slots
SET weekday = $2, start_time = $3, end_time = $4, location = $5,
    valid_from = $6, valid_until = $7, is_active = $8, updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: CloseGroupSlot :one
UPDATE group_slots SET valid_until = sqlc.arg(valid_until)::date, updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: DeactivateGroupSlot :one
UPDATE group_slots SET is_active = FALSE, updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: ListGroupSlots :many
SELECT gs.*, g.name AS group_name, s.name AS season_name
FROM group_slots gs
JOIN groups g ON g.id = gs.group_id
JOIN seasons s ON s.id = gs.season_id
WHERE gs.group_id = $1
ORDER BY gs.weekday, gs.start_time, gs.valid_from, gs.id;

-- name: ListGroupSlotsForSeason :many
SELECT gs.*, g.name AS group_name, s.name AS season_name
FROM group_slots gs
JOIN groups g ON g.id = gs.group_id
JOIN seasons s ON s.id = gs.season_id
WHERE gs.group_id = $1 AND gs.season_id = $2
ORDER BY gs.weekday, gs.start_time, gs.valid_from, gs.id;

-- name: ListCurrentGroupSlots :many
SELECT gs.*, g.name AS group_name, s.name AS season_name
FROM group_slots gs
JOIN groups g ON g.id = gs.group_id
JOIN seasons s ON s.id = gs.season_id
WHERE gs.group_id = $1 AND gs.season_id = $2
  AND gs.is_active AND gs.valid_from <= CURRENT_DATE
  AND (gs.valid_until IS NULL OR gs.valid_until >= CURRENT_DATE)
ORDER BY gs.weekday, gs.start_time, gs.valid_from, gs.id;

