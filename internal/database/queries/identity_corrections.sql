-- name: CreateIdentityCorrection :one
INSERT INTO identity_correction_requests(person_id,requesting_user_id,original_first_name,original_last_name,original_birth_date,proposed_first_name,proposed_last_name,proposed_birth_date)
VALUES(sqlc.arg(person_id),sqlc.arg(requesting_user_id),sqlc.arg(original_first_name),sqlc.arg(original_last_name),sqlc.arg(original_birth_date),sqlc.arg(proposed_first_name),sqlc.arg(proposed_last_name),sqlc.arg(proposed_birth_date)) RETURNING id;

-- name: ListIdentityCorrections :many
SELECT c.*,p.first_name,p.last_name,u.username FROM identity_correction_requests c
JOIN persons p ON p.id=c.person_id JOIN users u ON u.id=c.requesting_user_id
ORDER BY (c.status='pending') DESC,c.created_at DESC,c.id DESC;

-- name: GetIdentityCorrection :one
SELECT c.*,p.first_name,p.last_name,p.birth_date,p.archived_at,u.username FROM identity_correction_requests c
JOIN persons p ON p.id=c.person_id JOIN users u ON u.id=c.requesting_user_id WHERE c.id=sqlc.arg(id);

-- name: CountPendingIdentityCorrections :one
SELECT CAST(count(*) AS BIGINT) FROM identity_correction_requests WHERE status='pending';

-- name: ReviewIdentityCorrection :execrows
UPDATE identity_correction_requests SET status=sqlc.arg(status),reviewed_at=strftime('%Y-%m-%d %H:%M:%f','now'),reviewed_by_user_id=sqlc.arg(reviewed_by_user_id)
WHERE id=sqlc.arg(id) AND status='pending';

-- name: ApplyIdentityCorrection :execrows
UPDATE persons SET first_name=sqlc.arg(first_name),last_name=sqlc.arg(last_name),birth_date=sqlc.arg(birth_date),updated_at=strftime('%Y-%m-%d %H:%M:%f','now')
WHERE id=sqlc.arg(id) AND archived_at IS NULL;
