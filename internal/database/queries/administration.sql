-- name: AdministrativeCounts :one
SELECT
 (SELECT CAST(count(*) AS BIGINT) FROM trial_registrations ct WHERE ct.trial_date=sqlc.arg(today) AND ct.status='registered') AS today_trials,
 (SELECT CAST(count(*) AS BIGINT) FROM trial_registrations ct WHERE ct.trial_date>sqlc.arg(today) AND ct.status='registered') AS upcoming_trials,
 (SELECT CAST(count(*) AS BIGINT) FROM memberships WHERE status='pending') AS pending_memberships;

-- name: SearchAdministrativePersons :many
SELECT CAST(coalesce(lm.id,0) AS INTEGER) AS membership_id, coalesce(lm_season.name,'') AS membership_season, coalesce(lm.status,'') AS membership_status,
 coalesce(lt.id,0) AS trial_id, lt.trial_date AS last_trial_date,
 p.id,p.first_name,p.last_name,p.birth_date,p.email,p.phone_number,p.address,p.created_at,
 EXISTS(SELECT 1 FROM trial_registrations t WHERE t.person_id=p.id) AS has_trial,
 EXISTS(SELECT 1 FROM memberships m WHERE m.person_id=p.id) AS has_membership, CAST(EXISTS(SELECT 1 FROM person_guardians g WHERE g.guardian_person_id=p.id) AS BOOLEAN) AS is_guardian
FROM persons p
LEFT JOIN memberships lm ON lm.id=(SELECT m.id FROM memberships m JOIN seasons s ON s.id=m.season_id WHERE m.person_id=p.id
 AND (sqlc.arg(category)<>'members' OR (m.status='active' AND s.is_active AND (sqlc.arg(today)>=s.starts_at AND sqlc.arg(today)<=s.ends_at)))
 ORDER BY s.starts_at DESC,m.id DESC LIMIT 1)
LEFT JOIN seasons lm_season ON lm_season.id=lm.season_id
LEFT JOIN trial_registrations lt ON lt.id=(SELECT t.id FROM trial_registrations t WHERE t.person_id=p.id ORDER BY t.trial_date DESC,t.id DESC LIMIT 1)
WHERE p.archived_at IS NULL AND
 (sqlc.arg(search)='' OR instr(unicode_lower(p.first_name||' '||p.last_name||' '||coalesce(p.email,'')||' '||coalesce(p.phone_number,'')),unicode_lower(sqlc.arg(search)))>0
 OR (sqlc.arg(phone)<>'' AND instr(normalize_phone(coalesce(p.phone_number,'')),sqlc.arg(phone))>0))
AND (sqlc.arg(category)='' OR
 (sqlc.arg(category)='members' AND EXISTS(SELECT 1 FROM memberships m JOIN seasons s ON s.id=m.season_id WHERE m.person_id=p.id AND m.status='active' AND s.is_active AND (sqlc.arg(today)>=s.starts_at AND sqlc.arg(today)<=s.ends_at))) OR
 (sqlc.arg(category)='memberships' AND EXISTS(SELECT 1 FROM memberships m WHERE m.person_id=p.id)) OR
 (sqlc.arg(category)='prospects' AND EXISTS(SELECT 1 FROM trial_registrations t WHERE t.person_id=p.id) AND NOT EXISTS(SELECT 1 FROM memberships m WHERE m.person_id=p.id)) OR
 (sqlc.arg(category)='guardians' AND EXISTS(SELECT 1 FROM person_guardians g WHERE g.guardian_person_id=p.id)))
ORDER BY p.last_name,p.first_name,p.id LIMIT 51 OFFSET sqlc.arg(page_offset);

-- name: RecentAdministrativePersons :many
SELECT id,first_name,last_name FROM persons WHERE archived_at IS NULL ORDER BY created_at DESC,id DESC LIMIT 8;

-- name: AdministrativePerson :one
SELECT id,first_name,last_name,birth_date,email,phone_number,address,notes,archived_at FROM persons WHERE id=sqlc.arg(id);

-- name: AdministrativeRelations :many
SELECT p.id,p.first_name,p.last_name,p.email,p.phone_number,r.relationship_type,r.is_primary_contact,
 EXISTS(SELECT 1 FROM users u WHERE u.person_id=p.id) AS account_exists,
 EXISTS(SELECT 1 FROM users u WHERE u.person_id=p.id AND u.is_active) AS account_active,
 EXISTS(SELECT 1 FROM users u WHERE u.person_id=p.id AND u.activated_at IS NOT NULL AND u.password_hash IS NOT NULL) AS account_activated,
 EXISTS(SELECT 1 FROM guardian_access_grants ga WHERE ga.child_person_id=r.child_person_id AND ga.guardian_person_id=r.guardian_person_id AND ga.revoked_at IS NULL) AS has_access,
 EXISTS(SELECT 1 FROM person_emergency_contacts ec WHERE ec.person_id=sqlc.arg(person_id) AND ec.contact_person_id=p.id) AS is_emergency, CAST((r.child_person_id=sqlc.arg(person_id)) AS BOOLEAN) AS is_guardian, CAST((p.archived_at IS NOT NULL) AS BOOLEAN) AS archived
FROM person_guardians r JOIN persons p ON p.id=CASE WHEN r.child_person_id=sqlc.arg(person_id) THEN r.guardian_person_id ELSE r.child_person_id END
WHERE r.child_person_id=sqlc.arg(person_id) OR r.guardian_person_id=sqlc.arg(person_id)
ORDER BY p.last_name,p.first_name,r.id;

-- name: AdministrativeTrials :many
SELECT t.id,t.person_id,p.first_name,p.last_name,p.birth_date,p.email,p.phone_number, CAST(COALESCE(p.birth_date > date(t.trial_date,'-18 years','floor'),false) AS BOOLEAN) AS is_minor,
 t.activity_id,a.name AS activity_name,t.group_id,coalesce(g.name,'') AS group_name,
 t.group_slot_id,CAST(coalesce(CAST(substr(gs.start_time,1,5) AS TEXT),'') AS TEXT) AS start_time,CAST(coalesce(CAST(substr(gs.end_time,1,5) AS TEXT),'') AS TEXT) AS end_time,
 coalesce(gs.practice_label,'') AS practice_label,
 coalesce((SELECT l.name FROM locations l WHERE l.id=gs.location_id),gs.location,'') AS location, CAST(coalesce((SELECT l.address FROM locations l WHERE l.id=gs.location_id),'') AS TEXT) AS location_address,
 t.trial_date,t.status,t.notes,t.revision,
 CASE WHEN CAST(sqlc.arg(management_order) AS INTEGER)=1 THEN t.trial_date END AS management_date,
 (t.trial_date<sqlc.arg(today)) AS is_past,
 CASE WHEN t.trial_date>=sqlc.arg(today) THEN t.trial_date END AS upcoming_date,
 CASE WHEN t.trial_date<sqlc.arg(today) THEN t.trial_date END AS past_date, CAST(coalesce((SELECT m.id FROM memberships m JOIN seasons s ON s.id=m.season_id
   WHERE m.source_trial_id=t.id OR (m.person_id=t.person_id AND
     (m.season_id=gs.season_id OR (gs.id IS NULL AND t.trial_date BETWEEN s.starts_at AND s.ends_at)))
   ORDER BY (m.source_trial_id=t.id) DESC NULLS LAST,m.id DESC LIMIT 1),0) AS INTEGER) AS membership_id,
 coalesce(gs.season_id,(SELECT s.id FROM seasons s WHERE s.is_active AND t.trial_date BETWEEN s.starts_at AND s.ends_at ORDER BY s.starts_at DESC,s.id LIMIT 1),0) AS suggested_season_id
FROM trial_registrations t JOIN persons p ON p.id=t.person_id JOIN activities a ON a.id=t.activity_id
LEFT JOIN groups g ON g.id=t.group_id LEFT JOIN group_slots gs ON gs.id=t.group_slot_id
WHERE (CAST(sqlc.arg(person_id) AS INTEGER)=0 OR t.person_id=sqlc.arg(person_id))
AND (CAST(sqlc.arg(trial_id) AS INTEGER)=0 OR t.id=sqlc.arg(trial_id))
AND (sqlc.narg(range_start) IS NULL OR t.trial_date>=sqlc.narg(range_start))
AND (sqlc.narg(range_end) IS NULL OR t.trial_date<sqlc.narg(range_end))
AND (sqlc.narg(on_date) IS NULL OR t.trial_date=sqlc.narg(on_date))
AND (sqlc.narg(from_date) IS NULL OR (t.trial_date>=sqlc.narg(from_date) AND t.status='registered'))
AND (CAST(sqlc.arg(search) AS TEXT)='' OR instr(unicode_lower(p.first_name||' '||p.last_name),unicode_lower(sqlc.arg(search)))>0)
ORDER BY management_date DESC,
 is_past,upcoming_date ASC,past_date DESC,
 gs.start_time NULLS LAST, p.last_name,p.first_name,t.id LIMIT 101 OFFSET sqlc.arg(page_offset);

-- name: AdministrativeMemberships :many
SELECT m.id,m.status,m.season_id,m.source_trial_id,m.requested_at,m.approved_at,s.name AS season_name,t.name AS type_name,
 CAST((SELECT json_group_array(value) FROM (SELECT g.name AS value FROM membership_groups mg JOIN groups g ON g.id=mg.group_id WHERE mg.membership_id=m.id AND g.is_active AND mg.joined_at<=sqlc.arg(today) AND (mg.left_at IS NULL OR mg.left_at>sqlc.arg(today)) ORDER BY g.name)) AS JSON_TEXT_STRINGS) AS "groups"
FROM memberships m JOIN seasons s ON s.id=m.season_id JOIN membership_types t ON t.id=m.membership_type_id
WHERE m.person_id=sqlc.arg(person_id) ORDER BY s.starts_at DESC,m.id DESC;

-- name: AdministrativeActivities :many
SELECT id,name FROM activities WHERE is_active ORDER BY name,id;
-- name: AdministrativeSeasons :many
SELECT id,name FROM seasons WHERE is_active ORDER BY starts_at DESC,id;
-- name: AdministrativeMembershipTypes :many
SELECT id,name FROM membership_types WHERE is_active ORDER BY name,id;
-- name: AdministrativeSlots :many
SELECT gs.id,gs.group_id,gs.season_id,s.is_active AS season_active,s.ends_at AS season_ends_at,gs.valid_until,g.name AS group_name,s.name AS season_name,gs.weekday,CAST(substr(gs.start_time,1,5) AS TEXT) AS start_time,
 CAST(substr(gs.end_time,1,5) AS TEXT) AS end_time,coalesce((SELECT l.name FROM locations l WHERE l.id=gs.location_id),gs.location,'') AS location
FROM group_slots gs JOIN groups g ON g.id=gs.group_id JOIN seasons s ON s.id=gs.season_id WHERE gs.is_active AND g.is_active ORDER BY g.name,s.starts_at DESC,gs.weekday,gs.start_time;

-- name: LockAdministrativePerson :one
SELECT id FROM persons WHERE id=sqlc.arg(id) AND archived_at IS NULL;
-- name: UpdateAdministrativePersonNotes :exec
UPDATE persons SET notes=sqlc.arg(notes),updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id);
-- name: UpdateAdministrativeMembershipNotes :exec
UPDATE memberships SET admin_note=sqlc.arg(admin_note),updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id);
-- name: LockAdministrativeTrial :one
SELECT * FROM trial_registrations WHERE id=sqlc.arg(id);
-- name: LockAdministrativeMembership :one
SELECT * FROM memberships WHERE id=sqlc.arg(id);
-- name: CreateAdministrativeEvent :exec
INSERT INTO administrative_events(actor_user_id,action,resource_type,resource_id) VALUES(sqlc.arg(actor_user_id),sqlc.arg(action),sqlc.arg(resource_type),sqlc.arg(resource_id));

-- name: AdministrativePersonAccount :many
SELECT username,is_active, CAST((activated_at IS NOT NULL) AS BOOLEAN) AS activated
FROM users WHERE person_id=sqlc.arg(person_id);

-- name: EligibleMembershipSourceTrials :many
SELECT t.id,t.trial_date,a.name AS activity_name,coalesce(g.name,'') AS group_name,
 CAST(coalesce(CAST(substr(gs.start_time,1,5) AS TEXT),'') AS TEXT) AS start_time,
 CAST(coalesce(CAST(substr(gs.end_time,1,5) AS TEXT),'') AS TEXT) AS end_time
FROM trial_registrations t JOIN activities a ON a.id=t.activity_id
LEFT JOIN groups g ON g.id=t.group_id LEFT JOIN group_slots gs ON gs.id=t.group_slot_id
WHERE t.person_id=sqlc.arg(person_id) AND t.status='attended'
 AND NOT EXISTS(SELECT 1 FROM memberships m WHERE m.source_trial_id=t.id)
ORDER BY t.trial_date DESC,t.id DESC;

-- name: GetSeason :one
SELECT * FROM seasons WHERE id=sqlc.arg(id);

-- name: RepeatTrialPersons :many
SELECT p.id,p.first_name,p.last_name,t.id AS last_trial_id,t.trial_date AS last_trial_date
FROM persons p
JOIN trial_registrations t ON t.id=(SELECT latest.id FROM trial_registrations latest
 WHERE latest.person_id=p.id ORDER BY latest.trial_date DESC,latest.id DESC LIMIT 1)
WHERE p.archived_at IS NULL
 AND (sqlc.arg(search)='' OR instr(unicode_lower(p.first_name||' '||p.last_name),unicode_lower(sqlc.arg(search)))>0)
ORDER BY t.trial_date DESC,p.last_name,p.first_name,p.id
LIMIT 51 OFFSET sqlc.arg(page_offset);
