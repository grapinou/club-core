-- name: CreateRegistrationSubmission :one
INSERT INTO registration_submissions(first_name,last_name,birth_date,email,phone_number,address)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING *;

-- Matching includes archived Persons: archiving does not erase a durable identity.
-- name: ListIdentityMatchingPersons :many
SELECT id,first_name,last_name,birth_date,email,phone_number FROM persons ORDER BY id;

-- name: CreateRegistrationCandidate :exec
INSERT INTO registration_submission_candidates(submission_id,person_id,confidence,matched_name,matched_birth_date,matched_email,matched_phone)
VALUES ($1,$2,$3,$4,$5,$6,$7);

-- name: MarkRegistrationForReview :exec
UPDATE registration_submissions SET status='awaiting_identity_review',updated_at=clock_timestamp()
WHERE id=$1 AND status='received';

-- name: GetRegistrationSubmission :one
SELECT s.*, u.username AS resolver_username,
 EXISTS(SELECT 1 FROM registration_email_verifications v WHERE v.submission_id=s.id AND v.person_id=s.resolved_person_id AND v.used_at IS NOT NULL) AS email_verified
FROM registration_submissions s LEFT JOIN users u ON u.id=s.resolved_by_user_id
WHERE s.id=$1;

-- name: LockRegistrationSubmission :one
SELECT * FROM registration_submissions WHERE id=$1 FOR UPDATE;

-- name: GetRegistrationCandidate :one
SELECT * FROM registration_submission_candidates WHERE submission_id=$1 AND person_id=$2;

-- name: LockRegistrationPerson :one
SELECT id FROM persons WHERE id=$1 FOR KEY SHARE;

-- name: ResolveRegistrationSubmission :one
UPDATE registration_submissions
SET status='resolved',resolved_person_id=$2,resolution_type=$3,resolved_by_user_id=$4,
 resolved_at=clock_timestamp(),updated_at=clock_timestamp()
WHERE id=$1 AND status IN ('received','awaiting_identity_review','awaiting_email_verification')
RETURNING *;

-- name: ListRegistrationReviews :many
SELECT s.id,s.status,s.first_name,s.last_name,s.birth_date,s.created_at,
 COALESCE((SELECT a.last_error_code FROM registration_applications a WHERE a.submission_id=s.id),'')::text AS application_reason,
 EXISTS(SELECT 1 FROM registration_applications a WHERE a.submission_id=s.id AND a.status='needs_review') AS application_needs_review,
 count(c.person_id)::integer AS candidate_count,
 COALESCE(CASE max(CASE c.confidence WHEN 'strong' THEN 3 WHEN 'possible' THEN 2 WHEN 'weak' THEN 1 END)
 WHEN 3 THEN 'strong' WHEN 2 THEN 'possible' WHEN 1 THEN 'weak' END,'none')::text AS best_confidence
FROM registration_submissions s LEFT JOIN registration_submission_candidates c ON c.submission_id=s.id
GROUP BY s.id
ORDER BY CASE WHEN EXISTS(SELECT 1 FROM registration_applications a WHERE a.submission_id=s.id AND a.status='needs_review') THEN 0 ELSE 1 END,
 CASE s.status WHEN 'awaiting_identity_review' THEN 0 WHEN 'received' THEN 1 WHEN 'awaiting_email_verification' THEN 2 ELSE 3 END,
 CASE WHEN s.status IN ('received','awaiting_identity_review') THEN s.created_at END,
 CASE WHEN s.status NOT IN ('received','awaiting_identity_review') THEN s.updated_at END DESC,s.id;

-- name: CountOpenRegistrationReviews :one
SELECT count(*) FROM registration_submissions s WHERE status IN ('received','awaiting_identity_review') OR EXISTS(SELECT 1 FROM registration_applications a WHERE a.submission_id=s.id AND a.status='needs_review');

-- Only fields needed by authorized reviewers; no credentials or activation secrets.
-- name: ListRegistrationCandidates :many
SELECT c.*,p.first_name,p.last_name,p.birth_date,p.email,p.phone_number,p.address,p.archived_at,
 u.id AS user_id,u.is_active AS user_is_active,u.activated_at
FROM registration_submission_candidates c JOIN persons p ON p.id=c.person_id
LEFT JOIN users u ON u.person_id=p.id
WHERE c.submission_id=$1
ORDER BY CASE c.confidence WHEN 'strong' THEN 0 WHEN 'possible' THEN 1 ELSE 2 END,p.id;
