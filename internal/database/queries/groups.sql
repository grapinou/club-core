-- name: CreateGroup :one
INSERT INTO groups (activity_id, name, description, is_active)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetGroup :one
SELECT g.*, a.name AS activity_name
FROM groups g JOIN activities a ON a.id = g.activity_id
WHERE g.id = $1;

-- name: ListGroups :many
SELECT g.*, a.name AS activity_name
FROM groups g JOIN activities a ON a.id = g.activity_id
ORDER BY a.name, g.name, g.id;

-- name: ListActiveGroups :many
SELECT g.*, a.name AS activity_name
FROM groups g JOIN activities a ON a.id = g.activity_id
WHERE g.is_active
ORDER BY a.name, g.name, g.id;

-- name: UpdateGroup :one
UPDATE groups SET name = $2, description = $3, is_active = $4, updated_at = NOW()
WHERE id = $1 RETURNING *;
