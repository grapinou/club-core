-- All account mutations serialize on the session-selected User first.
-- name: LockSelfServiceAccount :one
SELECT u.id,u.person_id,u.password_hash,p.email
FROM users u JOIN persons p ON p.id=u.person_id
WHERE u.id=sqlc.arg(id) AND u.is_active AND u.activated_at IS NOT NULL
 AND u.password_hash IS NOT NULL AND p.archived_at IS NULL;

-- name: UpdateSelfServiceContact :exec
UPDATE persons SET phone_number=sqlc.arg(phone_number),address=sqlc.arg(address),updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id) AND archived_at IS NULL;

-- name: InvalidateEmailChanges :exec
UPDATE user_email_change_requests SET invalidated_at=strftime('%Y-%m-%d %H:%M:%f','now')
WHERE user_id=sqlc.arg(user_id) AND used_at IS NULL AND invalidated_at IS NULL;

-- name: CountRecentEmailChanges :one
SELECT CAST(count(*) AS BIGINT) FROM user_email_change_requests WHERE user_id=sqlc.arg(user_id) AND created_at>strftime('%Y-%m-%d %H:%M:%f','now','-1 hour');

-- name: CreateEmailChange :exec
INSERT INTO user_email_change_requests(user_id,person_id,new_email,new_email_normalized,new_email_hash,code_hash,expires_at)
VALUES(sqlc.arg(user_id),sqlc.arg(person_id),sqlc.arg(new_email),sqlc.arg(new_email_normalized),sqlc.arg(new_email_hash),sqlc.arg(code_hash),strftime('%Y-%m-%d %H:%M:%f','now',(sqlc.arg(ttl_seconds) + 0.0) || ' seconds'));

-- name: LockActiveEmailChange :one
SELECT id,new_email,code_hash FROM user_email_change_requests
WHERE user_id=sqlc.arg(user_id) AND person_id=sqlc.arg(person_id) AND used_at IS NULL AND invalidated_at IS NULL
AND expires_at>strftime('%Y-%m-%d %H:%M:%f','now');

-- name: ConsumeEmailChange :execrows
UPDATE user_email_change_requests SET used_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id) AND used_at IS NULL AND invalidated_at IS NULL AND expires_at>strftime('%Y-%m-%d %H:%M:%f','now');

-- name: UpdateSelfServiceEmail :exec
UPDATE persons SET email=sqlc.arg(email),updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id) AND archived_at IS NULL;

-- name: UpdateSelfServicePassword :exec
UPDATE users SET password_hash=sqlc.arg(password_hash) WHERE id=sqlc.arg(id);

-- name: CreateAccountSecurityEvent :exec
INSERT INTO account_security_events(user_id,person_id,event) VALUES(sqlc.arg(user_id),sqlc.arg(person_id),sqlc.arg(event));
