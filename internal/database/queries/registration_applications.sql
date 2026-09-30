-- name: ListRegistrationSeasons :many
SELECT * FROM seasons WHERE is_active ORDER BY starts_at DESC,id;
-- name: ListRegistrationMembershipTypes :many
SELECT * FROM membership_types WHERE is_active ORDER BY name,id;
-- name: ListRegistrationActivities :many
SELECT * FROM activities WHERE is_active ORDER BY name,id;
-- name: ListPresentedRegistrationConsents :many
SELECT * FROM consent_definitions WHERE id IN (sqlc.slice(ids)) ORDER BY code,version;
-- name: CreateRegistrationApplication :one
INSERT INTO registration_applications(submission_id,request_key,season_id,membership_type_id,required_activity_id)
VALUES (sqlc.arg(submission_id),sqlc.arg(request_key),sqlc.arg(season_id),sqlc.arg(membership_type_id),sqlc.arg(required_activity_id)) RETURNING *;
-- name: CreateRegistrationApplicationActivity :exec
INSERT INTO registration_application_activities(application_id,activity_id) VALUES (sqlc.arg(application_id),sqlc.arg(activity_id));
-- name: CreateRegistrationApplicationConsent :exec
INSERT INTO registration_application_consents(application_id,consent_definition_id,decision,presented_at) VALUES (sqlc.arg(application_id),sqlc.arg(consent_definition_id),sqlc.arg(decision),sqlc.arg(presented_at));
-- name: LockRegistrationApplication :one
SELECT * FROM registration_applications WHERE submission_id=sqlc.arg(submission_id);
-- name: GetRegistrationApplicationByRequest :one
SELECT * FROM registration_applications WHERE request_key=sqlc.arg(request_key);
-- name: GetRegistrationApplicationDetails :one
SELECT a.*,s.name AS season_name,t.name AS membership_type_name
FROM registration_applications a JOIN seasons s ON s.id=a.season_id JOIN membership_types t ON t.id=a.membership_type_id
WHERE a.submission_id=sqlc.arg(submission_id);
-- name: ListRegistrationApplicationActivities :many
SELECT a.* FROM registration_application_activities r JOIN activities a ON a.id=r.activity_id WHERE r.application_id=sqlc.arg(application_id) ORDER BY a.name,a.id;
-- name: ListRegistrationApplicationConsents :many
SELECT r.*,d.code,d.version,d.title,d.description FROM registration_application_consents r
JOIN consent_definitions d ON d.id=r.consent_definition_id WHERE r.application_id=sqlc.arg(application_id) ORDER BY d.code,d.version;
-- name: MarkRegistrationApplicationCreated :exec
UPDATE registration_applications SET status='membership_created',membership_id=sqlc.arg(membership_id),finalized_at=strftime('%Y-%m-%d %H:%M:%f','now'),updated_at=strftime('%Y-%m-%d %H:%M:%f','now'),last_error_code=NULL WHERE id=sqlc.arg(id);
-- name: MarkRegistrationApplicationReview :exec
UPDATE registration_applications SET status='needs_review',last_error_code=sqlc.arg(last_error_code),updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id);

-- name: GetVerifiedRegistrationApplicationStatus :one
SELECT a.status FROM registration_email_verifications v JOIN registration_applications a ON a.submission_id=v.submission_id
WHERE v.public_reference=sqlc.arg(public_reference) AND v.used_at IS NOT NULL;
