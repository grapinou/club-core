-- name: CreateGroupSlot :one
INSERT INTO group_slots (group_id, season_id, weekday, start_time, end_time, location, valid_from, valid_until, is_active, location_id, practice_label)
VALUES (sqlc.arg(group_id), sqlc.arg(season_id), sqlc.arg(weekday), sqlc.arg(start_time), sqlc.arg(end_time), sqlc.arg(location), sqlc.arg(valid_from), sqlc.arg(valid_until), sqlc.arg(is_active), sqlc.arg(location_id), sqlc.arg(practice_label)) RETURNING *;

-- name: UpdateGroupSlot :one
UPDATE group_slots
SET weekday = sqlc.arg(weekday), start_time = sqlc.arg(start_time), end_time = sqlc.arg(end_time), location = sqlc.arg(location), location_id = sqlc.arg(location_id), practice_label = sqlc.arg(practice_label),
    valid_from = sqlc.arg(valid_from), valid_until = sqlc.arg(valid_until), is_active = sqlc.arg(is_active), updated_at = strftime('%Y-%m-%d %H:%M:%f','now')
WHERE id = sqlc.arg(id) RETURNING *;

-- name: CloseGroupSlot :one
UPDATE group_slots SET valid_until = sqlc.arg(valid_until), updated_at = strftime('%Y-%m-%d %H:%M:%f','now')
WHERE id = sqlc.arg(id) RETURNING *;

-- name: DeactivateGroupSlot :one
UPDATE group_slots SET is_active = FALSE, updated_at = strftime('%Y-%m-%d %H:%M:%f','now')
WHERE id = sqlc.arg(id) RETURNING *;

-- name: ListGroupSlots :many
SELECT gs.*, g.name AS group_name, s.name AS season_name
FROM group_slots gs
JOIN groups g ON g.id = gs.group_id
JOIN seasons s ON s.id = gs.season_id
WHERE gs.group_id = sqlc.arg(group_id)
ORDER BY gs.weekday, gs.start_time, gs.valid_from, gs.id;

-- name: ListGroupSlotsForSeason :many
SELECT gs.*, g.name AS group_name, s.name AS season_name
FROM group_slots gs
JOIN groups g ON g.id = gs.group_id
JOIN seasons s ON s.id = gs.season_id
WHERE gs.group_id = sqlc.arg(group_id) AND gs.season_id = sqlc.arg(season_id)
ORDER BY gs.weekday, gs.start_time, gs.valid_from, gs.id;

-- name: ListCurrentGroupSlots :many
SELECT gs.*, g.name AS group_name, s.name AS season_name
FROM group_slots gs
JOIN groups g ON g.id = gs.group_id
JOIN seasons s ON s.id = gs.season_id
WHERE gs.group_id = sqlc.arg(group_id) AND gs.season_id = sqlc.arg(season_id)
  AND gs.is_active AND gs.valid_from <= CURRENT_DATE
  AND (gs.valid_until IS NULL OR gs.valid_until >= CURRENT_DATE)
ORDER BY gs.weekday, gs.start_time, gs.valid_from, gs.id;

