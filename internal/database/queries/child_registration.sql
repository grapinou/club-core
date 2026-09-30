-- name: CreateGuardianIdentityClaim :one
INSERT INTO guardian_identity_claims(first_name,last_name,birth_date,email,phone_number,address) VALUES(sqlc.arg(first_name),sqlc.arg(last_name),sqlc.arg(birth_date),sqlc.arg(email),sqlc.arg(phone_number),sqlc.arg(address)) RETURNING *;
-- name: CreateGuardianIdentityCandidate :exec
INSERT INTO guardian_identity_claim_candidates(guardian_claim_id,person_id,confidence,matched_name,matched_birth_date,matched_email,matched_phone) VALUES(sqlc.arg(guardian_claim_id),sqlc.arg(person_id),sqlc.arg(confidence),sqlc.arg(matched_name),sqlc.arg(matched_birth_date),sqlc.arg(matched_email),sqlc.arg(matched_phone));
-- name: MarkGuardianIdentityReview :exec
UPDATE guardian_identity_claims SET status='awaiting_review',updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id) AND status='received';
-- name: ResolveGuardianIdentityClaim :exec
UPDATE guardian_identity_claims SET status='resolved',resolved_person_id=sqlc.arg(resolved_person_id),resolution_type=sqlc.arg(resolution_type),resolved_by_user_id=sqlc.arg(resolved_by_user_id),resolved_at=strftime('%Y-%m-%d %H:%M:%f','now'),updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id) AND status IN ('received','awaiting_review');
-- name: GetGuardianIdentityClaim :one
SELECT * FROM guardian_identity_claims WHERE id=sqlc.arg(id);
-- name: LockGuardianIdentityClaim :one
SELECT * FROM guardian_identity_claims WHERE id=sqlc.arg(id);
-- name: ListGuardianIdentityCandidates :many
SELECT c.*,p.first_name,p.last_name,p.birth_date,p.email,p.phone_number,p.address,p.archived_at
FROM guardian_identity_claim_candidates c JOIN persons p ON p.id=c.person_id WHERE c.guardian_claim_id=sqlc.arg(guardian_claim_id) ORDER BY p.id;
-- name: CreateChildRegistrationApplication :exec
INSERT INTO child_registration_applications(application_id,guardian_claim_id,relationship_type,emergency_contact_requested) VALUES(sqlc.arg(application_id),sqlc.arg(guardian_claim_id),sqlc.arg(relationship_type),sqlc.arg(emergency_contact_requested));
-- name: GetChildRegistrationApplication :one
SELECT * FROM child_registration_applications WHERE application_id=sqlc.arg(application_id);
-- name: ConfirmChildRegistrationGuardian :exec
UPDATE child_registration_applications SET guardian_confirmed_at=strftime('%Y-%m-%d %H:%M:%f','now'),guardian_confirmed_by_user_id=sqlc.arg(guardian_confirmed_by_user_id) WHERE application_id=sqlc.arg(application_id) AND guardian_confirmed_at IS NULL;
