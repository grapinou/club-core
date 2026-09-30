-- name: ListUserRoles :many
SELECT r.* FROM roles r JOIN user_roles ur ON ur.role_id=r.id
WHERE ur.user_id=sqlc.arg(user_id) ORDER BY r.name;

-- name: ListAvailableRoles :many
SELECT * FROM roles ORDER BY name;

-- name: GetRoleByName :one
SELECT * FROM roles WHERE name=sqlc.arg(name);

-- name: AssignUserRole :exec
INSERT INTO user_roles(user_id,role_id) VALUES (sqlc.arg(user_id),sqlc.arg(role_id))
ON CONFLICT (user_id,role_id) DO NOTHING;

-- name: RevokeUserRole :exec
DELETE FROM user_roles WHERE user_id=sqlc.arg(user_id) AND role_id=sqlc.arg(role_id);
