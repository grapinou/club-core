-- name: ClaimRegistrationVerificationJob :one
UPDATE registration_verification_outbox SET status='processing',
 lease_until=strftime('%Y-%m-%d %H:%M:%f','now',(sqlc.arg(lease_seconds) + 0.0) || ' seconds'),
 lease_version=lease_version+1,updated_at=strftime('%Y-%m-%d %H:%M:%f','now')
WHERE id=(SELECT id FROM registration_verification_outbox WHERE (status='pending' AND available_at<=strftime('%Y-%m-%d %H:%M:%f','now')) OR (status='processing' AND lease_until<=strftime('%Y-%m-%d %H:%M:%f','now')) ORDER BY available_at,id LIMIT 1) RETURNING *;

-- name: CountRegistrationVerificationJobs :many
SELECT status,CAST(count(*) AS BIGINT) AS job_count FROM registration_verification_outbox GROUP BY status ORDER BY status;
