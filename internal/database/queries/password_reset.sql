-- name: PasswordResetUser :one
SELECT id,person_id,username FROM users
WHERE id=sqlc.arg(id) AND is_active AND activated_at IS NOT NULL
 AND password_hash IS NOT NULL AND length(password_hash)>0;

-- name: PasswordResetUserForPerson :one
SELECT id FROM users WHERE person_id=sqlc.arg(person_id);

-- name: CountRecentPasswordResets :one
SELECT CAST(count(*) AS BIGINT) FROM user_password_reset_requests
WHERE user_id=sqlc.arg(user_id) AND created_at>strftime('%Y-%m-%d %H:%M:%f','now','-1 hour');

-- name: InvalidatePasswordResets :exec
UPDATE user_password_reset_requests SET invalidated_at=strftime('%Y-%m-%d %H:%M:%f','now')
WHERE user_id=sqlc.arg(user_id) AND used_at IS NULL AND invalidated_at IS NULL;

-- name: CreatePasswordReset :one
INSERT INTO user_password_reset_requests(user_id,requested_by_user_id,token_hash,expires_at)
VALUES(sqlc.arg(user_id),sqlc.narg(requested_by_user_id),sqlc.arg(token_hash),
 strftime('%Y-%m-%d %H:%M:%f','now',(sqlc.arg(ttl_seconds)+0.0)||' seconds')) RETURNING id;

-- name: ActivePasswordReset :one
SELECT r.id,r.user_id,u.person_id,u.username FROM user_password_reset_requests r
JOIN users u ON u.id=r.user_id
WHERE r.token_hash=sqlc.arg(token_hash) AND r.used_at IS NULL AND r.invalidated_at IS NULL
 AND r.expires_at>strftime('%Y-%m-%d %H:%M:%f','now')
 AND u.is_active AND u.activated_at IS NOT NULL AND u.password_hash IS NOT NULL AND length(u.password_hash)>0;

-- name: ConsumePasswordReset :execrows
UPDATE user_password_reset_requests SET used_at=strftime('%Y-%m-%d %H:%M:%f','now')
WHERE id=sqlc.arg(id) AND used_at IS NULL AND invalidated_at IS NULL
 AND expires_at>strftime('%Y-%m-%d %H:%M:%f','now');

-- name: InvalidateFailedPasswordReset :exec
UPDATE user_password_reset_requests SET invalidated_at=strftime('%Y-%m-%d %H:%M:%f','now')
WHERE id=sqlc.arg(id) AND used_at IS NULL AND invalidated_at IS NULL;
