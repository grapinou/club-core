-- name: CreateGuardianIdentityClaim :one
INSERT INTO guardian_identity_claims(first_name,last_name,birth_date,email,phone_number,address) VALUES($1,$2,$3,$4,$5,$6) RETURNING *;
-- name: CreateGuardianIdentityCandidate :exec
INSERT INTO guardian_identity_claim_candidates(guardian_claim_id,person_id,confidence,matched_name,matched_birth_date,matched_email,matched_phone) VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: MarkGuardianIdentityReview :exec
UPDATE guardian_identity_claims SET status='awaiting_review',updated_at=clock_timestamp() WHERE id=$1 AND status='received';
-- name: ResolveGuardianIdentityClaim :exec
UPDATE guardian_identity_claims SET status='resolved',resolved_person_id=$2,resolution_type=$3,resolved_by_user_id=$4,resolved_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 AND status IN ('received','awaiting_review');
-- name: GetGuardianIdentityClaim :one
SELECT * FROM guardian_identity_claims WHERE id=$1;
-- name: LockGuardianIdentityClaim :one
SELECT * FROM guardian_identity_claims WHERE id=$1 FOR UPDATE;
-- name: ListGuardianIdentityCandidates :many
SELECT c.*,p.first_name,p.last_name,p.birth_date,p.email,p.phone_number,p.address,p.archived_at
FROM guardian_identity_claim_candidates c JOIN persons p ON p.id=c.person_id WHERE c.guardian_claim_id=$1 ORDER BY p.id;
-- name: CreateChildRegistrationApplication :exec
INSERT INTO child_registration_applications(application_id,guardian_claim_id,relationship_type,emergency_contact_requested) VALUES($1,$2,$3,$4);
-- name: GetChildRegistrationApplication :one
SELECT * FROM child_registration_applications WHERE application_id=$1;
-- name: ConfirmChildRegistrationGuardian :exec
UPDATE child_registration_applications SET guardian_confirmed_at=clock_timestamp(),guardian_confirmed_by_user_id=$2 WHERE application_id=$1 AND guardian_confirmed_at IS NULL;
