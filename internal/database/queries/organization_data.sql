-- name: GetActiveOrganization :one
SELECT * FROM organizations WHERE is_active;

-- name: CreateOrganization :one
INSERT INTO organizations (name,short_name,description,public_email,public_phone,correspondence_address,website_url)
VALUES (sqlc.arg(name),sqlc.arg(short_name),sqlc.arg(description),sqlc.arg(public_email),sqlc.arg(public_phone),sqlc.arg(correspondence_address),sqlc.arg(website_url)) RETURNING *;

-- name: UpdateOrganization :one
UPDATE organizations SET name=sqlc.arg(name),short_name=sqlc.arg(short_name),description=sqlc.arg(description),public_email=sqlc.arg(public_email),public_phone=sqlc.arg(public_phone),
 correspondence_address=sqlc.arg(correspondence_address),website_url=sqlc.arg(website_url),is_active=sqlc.arg(is_active),updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id) RETURNING *;

-- name: CreateLocation :one
INSERT INTO locations (organization_id,name,address) VALUES (sqlc.arg(organization_id),sqlc.arg(name),sqlc.arg(address)) RETURNING *;

-- name: UpdateLocation :one
UPDATE locations SET name=sqlc.arg(name),address=sqlc.arg(address),is_active=sqlc.arg(is_active),updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id) RETURNING *;

-- name: ListOrganizationLocations :many
SELECT * FROM locations WHERE organization_id=sqlc.arg(organization_id) ORDER BY name,id;

-- name: CreateOrganizationLink :one
INSERT INTO organization_links (organization_id,kind,label,url,position) VALUES (sqlc.arg(organization_id),sqlc.arg(kind),sqlc.arg(label),sqlc.arg(url),sqlc.arg(position)) RETURNING *;

-- name: UpdateOrganizationLink :one
UPDATE organization_links SET kind=sqlc.arg(kind),label=sqlc.arg(label),url=sqlc.arg(url),position=sqlc.arg(position),is_active=sqlc.arg(is_active),updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id) RETURNING *;

-- name: ListOrganizationLinks :many
SELECT * FROM organization_links WHERE organization_id=sqlc.arg(organization_id) ORDER BY position,id;

-- name: SetGroupSlotLocation :one
UPDATE group_slots SET location_id=sqlc.arg(location_id),location=NULL,updated_at=strftime('%Y-%m-%d %H:%M:%f','now') WHERE id=sqlc.arg(id) RETURNING *;

-- name: ListReferenceSchedule :many
SELECT gs.id,gs.group_id,gs.season_id,g.name AS group_name,a.name AS activity_name,
 gs.weekday,CAST(substr(gs.start_time,1,5) AS TEXT) AS start_time,CAST(substr(gs.end_time,1,5) AS TEXT) AS end_time,
 gs.practice_label,gs.location_id,coalesce(l.name,gs.location,'') AS location_name, CAST(coalesce(l.address,'') AS TEXT) AS location_address,gs.valid_from,gs.valid_until
FROM group_slots gs JOIN groups g ON g.id=gs.group_id JOIN activities a ON a.id=g.activity_id
JOIN seasons s ON s.id=gs.season_id LEFT JOIN locations l ON l.id=gs.location_id
WHERE s.id=sqlc.arg(id) AND s.is_active AND gs.is_active AND g.is_active AND a.is_active
 AND (gs.location_id IS NULL OR l.is_active)
ORDER BY gs.weekday,gs.start_time,gs.id;

-- name: GetReferenceSeason :one
SELECT * FROM seasons WHERE name=sqlc.arg(name);

-- name: ListOrganizationPublicImages :many
SELECT * FROM organization_public_images WHERE organization_id=sqlc.arg(organization_id) ORDER BY placement;
