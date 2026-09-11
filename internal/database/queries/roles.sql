-- name: ListUserRoles :many
SELECT r.* FROM roles r JOIN user_roles ur ON ur.role_id=r.id
WHERE ur.user_id=$1 ORDER BY r.name;

-- name: ListAvailableRoles :many
SELECT * FROM roles ORDER BY name;

-- name: GetRoleByName :one
SELECT * FROM roles WHERE name=$1;

-- name: AssignUserRole :exec
INSERT INTO user_roles(user_id,role_id) VALUES ($1,$2)
ON CONFLICT (user_id,role_id) DO NOTHING;

-- name: RevokeUserRole :exec
DELETE FROM user_roles WHERE user_id=$1 AND role_id=$2;
