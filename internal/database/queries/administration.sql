-- name: AdministrativeCounts :one
SELECT
 (SELECT count(*) FROM trial_registrations WHERE trial_date=sqlc.arg(today)::date AND status='registered') AS today_trials,
 (SELECT count(*) FROM trial_registrations WHERE trial_date>sqlc.arg(today)::date AND status='registered') AS upcoming_trials,
 (SELECT count(*) FROM memberships WHERE status='pending') AS pending_memberships;

-- name: SearchAdministrativePersons :many
SELECT id,first_name,last_name,birth_date,email,phone_number,address,created_at
FROM persons WHERE archived_at IS NULL AND
 (sqlc.arg(search)::text='' OR position(lower(sqlc.arg(search)) in lower(first_name||' '||last_name||' '||coalesce(email,'')||' '||coalesce(phone_number,'')))>0
 OR (sqlc.arg(phone)::text<>'' AND position(sqlc.arg(phone) in regexp_replace(coalesce(phone_number,''),'[^0-9]','','g'))>0))
ORDER BY last_name,first_name,id LIMIT 51 OFFSET sqlc.arg(page_offset);

-- name: RecentAdministrativePersons :many
SELECT id,first_name,last_name FROM persons WHERE archived_at IS NULL ORDER BY created_at DESC,id DESC LIMIT 8;

-- name: AdministrativePerson :one
SELECT id,first_name,last_name,birth_date,email,phone_number,address,notes,archived_at FROM persons WHERE id=$1;

-- name: AdministrativeRelations :many
SELECT p.id,p.first_name,p.last_name,r.relationship_type,r.is_primary_contact,
 (r.child_person_id=sqlc.arg(person_id))::boolean AS is_guardian, (p.archived_at IS NOT NULL)::boolean AS archived
FROM person_guardians r JOIN persons p ON p.id=CASE WHEN r.child_person_id=sqlc.arg(person_id) THEN r.guardian_person_id ELSE r.child_person_id END
WHERE r.child_person_id=sqlc.arg(person_id) OR r.guardian_person_id=sqlc.arg(person_id)
ORDER BY p.last_name,p.first_name,r.id;

-- name: AdministrativeTrials :many
SELECT t.id,t.person_id,p.first_name,p.last_name,t.activity_id,a.name AS activity_name,t.group_id,coalesce(g.name,'')::text AS group_name,
 t.group_slot_id,coalesce(to_char(gs.start_time,'HH24:MI'),'')::text AS start_time,coalesce(to_char(gs.end_time,'HH24:MI'),'')::text AS end_time,
 coalesce(gs.location,'')::text AS location,t.trial_date,t.status,t.notes,t.revision
FROM trial_registrations t JOIN persons p ON p.id=t.person_id JOIN activities a ON a.id=t.activity_id
LEFT JOIN groups g ON g.id=t.group_id LEFT JOIN group_slots gs ON gs.id=t.group_slot_id
WHERE (sqlc.arg(person_id)::integer=0 OR t.person_id=sqlc.arg(person_id))
AND (sqlc.arg(trial_id)::integer=0 OR t.id=sqlc.arg(trial_id))
AND (sqlc.narg(on_date)::date IS NULL OR t.trial_date=sqlc.narg(on_date))
AND (sqlc.narg(from_date)::date IS NULL OR (t.trial_date>=sqlc.narg(from_date) AND t.status='registered'))
ORDER BY t.trial_date,t.id LIMIT 101;

-- name: AdministrativeMemberships :many
SELECT m.id,m.status,s.name AS season_name,t.name AS type_name,
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
 to_char(gs.end_time,'HH24:MI')::text AS end_time,coalesce(gs.location,'')::text AS location
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
