-- name: AdministrativeCounts :one
SELECT
 (SELECT count(*) FROM trial_registrations WHERE trial_date=sqlc.arg(today)::date AND status='registered') AS today_trials,
 (SELECT count(*) FROM trial_registrations WHERE trial_date>sqlc.arg(today)::date AND status='registered') AS upcoming_trials,
 (SELECT count(*) FROM trial_registrations WHERE trial_date<sqlc.arg(today)::date AND status='registered') AS past_pending_trials,
 (SELECT count(*) FROM memberships WHERE status='pending') AS pending_memberships;

-- name: SearchAdministrativePersons :many
SELECT p.id,p.first_name,p.last_name,p.birth_date,p.email,p.phone_number,p.address,p.created_at,
 EXISTS(SELECT 1 FROM trial_registrations t WHERE t.person_id=p.id)::boolean AS has_trial,
 EXISTS(SELECT 1 FROM memberships m WHERE m.person_id=p.id)::boolean AS has_membership,
 EXISTS(SELECT 1 FROM person_guardians g WHERE g.guardian_person_id=p.id)::boolean AS is_guardian
FROM persons p WHERE p.archived_at IS NULL AND
 (sqlc.arg(search)::text='' OR position(lower(sqlc.arg(search)) in lower(p.first_name||' '||p.last_name||' '||coalesce(p.email,'')||' '||coalesce(p.phone_number,'')))>0
 OR (sqlc.arg(phone)::text<>'' AND position(sqlc.arg(phone) in regexp_replace(coalesce(p.phone_number,''),'[^0-9]','','g'))>0))
ORDER BY p.last_name,p.first_name,p.id LIMIT 51 OFFSET sqlc.arg(page_offset);

-- name: RecentAdministrativePersons :many
SELECT id,first_name,last_name FROM persons WHERE archived_at IS NULL ORDER BY created_at DESC,id DESC LIMIT 8;

-- name: AdministrativePerson :one
SELECT id,first_name,last_name,birth_date,email,phone_number,address,notes,archived_at FROM persons WHERE id=$1;

-- name: AdministrativeRelations :many
SELECT p.id,p.first_name,p.last_name,p.email,p.phone_number,r.relationship_type,r.is_primary_contact,
 EXISTS(SELECT 1 FROM guardian_access_grants ga WHERE ga.child_person_id=r.child_person_id AND ga.guardian_person_id=r.guardian_person_id AND ga.revoked_at IS NULL)::boolean AS has_access,
 EXISTS(SELECT 1 FROM person_emergency_contacts ec WHERE ec.person_id=sqlc.arg(person_id) AND ec.contact_person_id=p.id)::boolean AS is_emergency,
 (r.child_person_id=sqlc.arg(person_id))::boolean AS is_guardian, (p.archived_at IS NOT NULL)::boolean AS archived
FROM person_guardians r JOIN persons p ON p.id=CASE WHEN r.child_person_id=sqlc.arg(person_id) THEN r.guardian_person_id ELSE r.child_person_id END
WHERE r.child_person_id=sqlc.arg(person_id) OR r.guardian_person_id=sqlc.arg(person_id)
ORDER BY p.last_name,p.first_name,r.id;

-- name: AdministrativeTrials :many
SELECT t.id,t.person_id,p.first_name,p.last_name,p.birth_date,p.email,p.phone_number,
 COALESCE(p.birth_date > t.trial_date - INTERVAL '18 years',false)::boolean AS is_minor,
 t.activity_id,a.name AS activity_name,t.group_id,coalesce(g.name,'')::text AS group_name,
 t.group_slot_id,coalesce(to_char(gs.start_time,'HH24:MI'),'')::text AS start_time,coalesce(to_char(gs.end_time,'HH24:MI'),'')::text AS end_time,
 coalesce(gs.practice_label,'')::text AS practice_label,
 coalesce((SELECT l.name FROM locations l WHERE l.id=gs.location_id),gs.location,'')::text AS location,
 coalesce((SELECT l.address FROM locations l WHERE l.id=gs.location_id),'')::text AS location_address,
 t.trial_date,t.status,t.notes,t.revision,
 coalesce((SELECT m.id FROM memberships m JOIN seasons s ON s.id=m.season_id
   WHERE m.source_trial_id=t.id OR (m.person_id=t.person_id AND
     (m.season_id=gs.season_id OR (gs.id IS NULL AND t.trial_date BETWEEN s.starts_at AND s.ends_at)))
   ORDER BY (m.source_trial_id=t.id) DESC NULLS LAST,m.id DESC LIMIT 1),0)::integer AS membership_id,
 coalesce(gs.season_id,(SELECT s.id FROM seasons s WHERE s.is_active AND t.trial_date BETWEEN s.starts_at AND s.ends_at ORDER BY s.starts_at DESC,s.id LIMIT 1),0)::integer AS suggested_season_id
FROM trial_registrations t JOIN persons p ON p.id=t.person_id JOIN activities a ON a.id=t.activity_id
LEFT JOIN groups g ON g.id=t.group_id LEFT JOIN group_slots gs ON gs.id=t.group_slot_id
WHERE (sqlc.arg(person_id)::integer=0 OR t.person_id=sqlc.arg(person_id))
AND (sqlc.arg(trial_id)::integer=0 OR t.id=sqlc.arg(trial_id))
AND (sqlc.narg(on_date)::date IS NULL OR t.trial_date=sqlc.narg(on_date))
AND (sqlc.narg(from_date)::date IS NULL OR (t.trial_date>=sqlc.narg(from_date) AND t.status='registered'))
AND (sqlc.narg(before_date)::date IS NULL OR (t.trial_date<sqlc.narg(before_date) AND t.status='registered'))
ORDER BY (t.trial_date<sqlc.arg(today)::date),
 CASE WHEN t.trial_date>=sqlc.arg(today)::date THEN t.trial_date END ASC,
 CASE WHEN t.trial_date<sqlc.arg(today)::date THEN t.trial_date END DESC,
 t.id LIMIT 101;

-- name: AdministrativeMemberships :many
SELECT m.id,m.status,m.season_id,m.source_trial_id,m.requested_at,m.approved_at,s.name AS season_name,t.name AS type_name,
 ARRAY(SELECT g.name FROM membership_groups mg JOIN groups g ON g.id=mg.group_id WHERE mg.membership_id=m.id AND g.is_active AND mg.joined_at<=sqlc.arg(today)::date AND (mg.left_at IS NULL OR mg.left_at>sqlc.arg(today)::date) ORDER BY g.name)::text[] AS groups
FROM memberships m JOIN seasons s ON s.id=m.season_id JOIN membership_types t ON t.id=m.membership_type_id
WHERE m.person_id=sqlc.arg(person_id) ORDER BY s.starts_at DESC,m.id DESC;

-- name: AdministrativeActivities :many
SELECT id,name FROM activities WHERE is_active ORDER BY name,id;
-- name: AdministrativeSeasons :many
SELECT id,name FROM seasons WHERE is_active ORDER BY starts_at DESC,id;
-- name: AdministrativeMembershipTypes :many
SELECT id,name FROM membership_types WHERE is_active ORDER BY name,id;
-- name: AdministrativeSlots :many
SELECT gs.id,g.name AS group_name,s.name AS season_name,gs.weekday,to_char(gs.start_time,'HH24:MI')::text AS start_time,
 to_char(gs.end_time,'HH24:MI')::text AS end_time,coalesce((SELECT l.name FROM locations l WHERE l.id=gs.location_id),gs.location,'')::text AS location
FROM group_slots gs JOIN groups g ON g.id=gs.group_id JOIN seasons s ON s.id=gs.season_id WHERE gs.is_active AND g.is_active ORDER BY g.name,s.starts_at DESC,gs.weekday,gs.start_time;

-- name: LockAdministrativePerson :one
SELECT id FROM persons WHERE id=$1 AND archived_at IS NULL FOR NO KEY UPDATE;
-- name: UpdateAdministrativePersonNotes :exec
UPDATE persons SET notes=$2,updated_at=clock_timestamp() WHERE id=$1;
-- name: UpdateAdministrativeMembershipNotes :exec
UPDATE memberships SET admin_note=$2,updated_at=clock_timestamp() WHERE id=$1;
-- name: LockAdministrativeTrial :one
SELECT * FROM trial_registrations WHERE id=$1 FOR UPDATE;
-- name: LockAdministrativeMembership :one
SELECT * FROM memberships WHERE id=$1 FOR UPDATE;
-- name: CreateAdministrativeEvent :exec
INSERT INTO administrative_events(actor_user_id,action,resource_type,resource_id) VALUES($1,$2,$3,$4);

-- name: AdministrativePersonAccount :many
SELECT username,is_active,(activated_at IS NOT NULL)::boolean AS activated
FROM users WHERE person_id=$1;
