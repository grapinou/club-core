-- name: GetActiveOrganization :one
SELECT * FROM organizations WHERE is_active;

-- name: CreateOrganization :one
INSERT INTO organizations (name,short_name,description,public_email,public_phone,correspondence_address,website_url)
VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING *;

-- name: UpdateOrganization :one
UPDATE organizations SET name=$2,short_name=$3,description=$4,public_email=$5,public_phone=$6,
 correspondence_address=$7,website_url=$8,is_active=$9,updated_at=NOW() WHERE id=$1 RETURNING *;

-- name: CreateLocation :one
INSERT INTO locations (organization_id,name,address) VALUES ($1,$2,$3) RETURNING *;

-- name: UpdateLocation :one
UPDATE locations SET name=$2,address=$3,is_active=$4,updated_at=NOW() WHERE id=$1 RETURNING *;

-- name: ListOrganizationLocations :many
SELECT * FROM locations WHERE organization_id=$1 ORDER BY name,id;

-- name: CreateOrganizationLink :one
INSERT INTO organization_links (organization_id,kind,label,url,position) VALUES ($1,$2,$3,$4,$5) RETURNING *;

-- name: UpdateOrganizationLink :one
UPDATE organization_links SET kind=$2,label=$3,url=$4,position=$5,is_active=$6,updated_at=NOW() WHERE id=$1 RETURNING *;

-- name: ListOrganizationLinks :many
SELECT * FROM organization_links WHERE organization_id=$1 ORDER BY position,id;

-- name: SetGroupSlotLocation :one
UPDATE group_slots SET location_id=$2,location=NULL,updated_at=NOW() WHERE id=$1 RETURNING *;

-- name: ListReferenceSchedule :many
SELECT gs.id,gs.group_id,gs.season_id,g.name AS group_name,a.name AS activity_name,
 gs.weekday,to_char(gs.start_time,'HH24:MI')::text AS start_time,to_char(gs.end_time,'HH24:MI')::text AS end_time,
 gs.practice_label,gs.location_id,coalesce(l.name,gs.location,'')::text AS location_name,
 coalesce(l.address,'')::text AS location_address,gs.valid_from,gs.valid_until
FROM group_slots gs JOIN groups g ON g.id=gs.group_id JOIN activities a ON a.id=g.activity_id
JOIN seasons s ON s.id=gs.season_id LEFT JOIN locations l ON l.id=gs.location_id
WHERE s.id=$1 AND s.is_active AND gs.is_active AND g.is_active AND a.is_active
 AND (gs.location_id IS NULL OR l.is_active)
ORDER BY gs.weekday,gs.start_time,gs.id;

-- name: GetReferenceSeason :one
SELECT * FROM seasons WHERE name=$1;

-- name: ListOrganizationPublicImages :many
SELECT * FROM organization_public_images WHERE organization_id=$1 ORDER BY placement;
