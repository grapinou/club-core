-- name: CreateGroup :one
INSERT INTO groups (activity_id, name, description, is_active)
VALUES (sqlc.arg(activity_id), sqlc.arg(name), sqlc.arg(description), sqlc.arg(is_active)) RETURNING *;

-- name: GetGroup :one
SELECT g.*, a.name AS activity_name
FROM groups g JOIN activities a ON a.id = g.activity_id
WHERE g.id = sqlc.arg(id);

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
UPDATE groups SET name = sqlc.arg(name), description = sqlc.arg(description), is_active = sqlc.arg(is_active), updated_at = strftime('%Y-%m-%d %H:%M:%f','now')
WHERE id = sqlc.arg(id) RETURNING *;
