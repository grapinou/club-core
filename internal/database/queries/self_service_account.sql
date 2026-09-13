-- All account mutations serialize on the session-selected User first.
-- name: LockSelfServiceAccount :one
SELECT u.id,u.person_id,u.password_hash,p.email
FROM users u JOIN persons p ON p.id=u.person_id
WHERE u.id=$1 AND u.is_active AND u.activated_at IS NOT NULL
 AND u.password_hash IS NOT NULL AND p.archived_at IS NULL FOR UPDATE OF u,p;

-- name: UpdateSelfServiceContact :exec
UPDATE persons SET phone_number=$2,address=$3,updated_at=clock_timestamp() WHERE id=$1 AND archived_at IS NULL;

-- name: InvalidateEmailChanges :exec
UPDATE user_email_change_requests SET invalidated_at=clock_timestamp()
WHERE user_id=$1 AND used_at IS NULL AND invalidated_at IS NULL;

-- name: CountRecentEmailChanges :one
SELECT count(*) FROM user_email_change_requests WHERE user_id=$1 AND created_at>clock_timestamp()-interval '1 hour';

-- name: CreateEmailChange :exec
INSERT INTO user_email_change_requests(user_id,person_id,new_email,new_email_normalized,new_email_hash,code_hash,expires_at)
VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()+make_interval(secs => sqlc.arg(ttl_seconds)::float8));

-- name: LockActiveEmailChange :one
SELECT id,new_email,code_hash FROM user_email_change_requests
WHERE user_id=$1 AND person_id=$2 AND used_at IS NULL AND invalidated_at IS NULL
AND expires_at>clock_timestamp() FOR UPDATE;

-- name: ConsumeEmailChange :execrows
UPDATE user_email_change_requests SET used_at=clock_timestamp() WHERE id=$1 AND used_at IS NULL AND invalidated_at IS NULL AND expires_at>clock_timestamp();

-- name: UpdateSelfServiceEmail :exec
UPDATE persons SET email=$2,updated_at=clock_timestamp() WHERE id=$1 AND archived_at IS NULL;

-- name: UpdateSelfServicePassword :exec
UPDATE users SET password_hash=$2 WHERE id=$1;

-- name: CreateAccountSecurityEvent :exec
INSERT INTO account_security_events(user_id,person_id,event) VALUES($1,$2,$3);
