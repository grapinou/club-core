-- name: ListRegistrationSeasons :many
SELECT * FROM seasons WHERE is_active ORDER BY starts_at DESC,id;
-- name: ListRegistrationMembershipTypes :many
SELECT * FROM membership_types WHERE is_active ORDER BY name,id;
-- name: ListRegistrationActivities :many
SELECT * FROM activities WHERE is_active ORDER BY name,id;
-- name: ListPresentedRegistrationConsents :many
SELECT * FROM consent_definitions WHERE id=ANY($1::integer[]) ORDER BY code,version;
-- name: CreateRegistrationApplication :one
INSERT INTO registration_applications(submission_id,request_key,season_id,membership_type_id)
VALUES ($1,$2,$3,$4) RETURNING *;
-- name: CreateRegistrationApplicationActivity :exec
INSERT INTO registration_application_activities(application_id,activity_id) VALUES ($1,$2);
-- name: CreateRegistrationApplicationConsent :exec
INSERT INTO registration_application_consents(application_id,consent_definition_id,decision,presented_at) VALUES ($1,$2,$3,$4);
-- name: LockRegistrationApplication :one
SELECT * FROM registration_applications WHERE submission_id=$1 FOR UPDATE;
-- name: GetRegistrationApplicationByRequest :one
SELECT * FROM registration_applications WHERE request_key=$1;
-- name: GetRegistrationApplicationDetails :one
SELECT a.*,s.name AS season_name,t.name AS membership_type_name
FROM registration_applications a JOIN seasons s ON s.id=a.season_id JOIN membership_types t ON t.id=a.membership_type_id
WHERE a.submission_id=$1;
-- name: ListRegistrationApplicationActivities :many
SELECT a.* FROM registration_application_activities r JOIN activities a ON a.id=r.activity_id WHERE r.application_id=$1 ORDER BY a.name,a.id;
-- name: ListRegistrationApplicationConsents :many
SELECT r.*,d.code,d.version,d.title,d.description FROM registration_application_consents r
JOIN consent_definitions d ON d.id=r.consent_definition_id WHERE r.application_id=$1 ORDER BY d.code,d.version;
-- name: MarkRegistrationApplicationCreated :exec
UPDATE registration_applications SET status='membership_created',membership_id=$2,finalized_at=clock_timestamp(),updated_at=clock_timestamp(),last_error_code=NULL WHERE id=$1;
-- name: MarkRegistrationApplicationReview :exec
UPDATE registration_applications SET status='needs_review',last_error_code=$2,updated_at=clock_timestamp() WHERE id=$1;

-- name: GetVerifiedRegistrationApplicationStatus :one
SELECT a.status FROM registration_email_verifications v JOIN registration_applications a ON a.submission_id=v.submission_id
WHERE v.public_reference=$1 AND v.used_at IS NOT NULL;
