-- name: ClaimRegistrationVerificationJob :one
WITH candidate AS (
 SELECT id FROM registration_verification_outbox
 WHERE (status='pending' AND available_at<=clock_timestamp())
    OR (status='processing' AND lease_until<=clock_timestamp())
 ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE registration_verification_outbox o SET status='processing',
 lease_until=clock_timestamp()+make_interval(secs=>sqlc.arg(lease_seconds)::double precision),
 lease_version=lease_version+1,updated_at=clock_timestamp()
FROM candidate WHERE o.id=candidate.id RETURNING o.*;

-- name: CountRegistrationVerificationJobs :many
SELECT status,count(*)::bigint AS job_count FROM registration_verification_outbox GROUP BY status ORDER BY status;
