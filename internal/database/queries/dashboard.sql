-- Read-only dashboard projections: no administrative dossier or credentials.
-- name: GetDashboardPerson :one
SELECT p.id,p.first_name,p.last_name FROM persons p JOIN users u ON u.person_id=p.id WHERE u.id=sqlc.arg(id);

-- name: ListDashboardMemberships :many
SELECT m.id,m.status,s.name AS season_name,t.name AS membership_type_name,
 CAST((SELECT json_group_array(value) FROM (SELECT a.name AS value FROM activities a JOIN membership_activities ma ON ma.activity_id=a.id WHERE ma.membership_id=m.id ORDER BY a.name)) AS JSON_TEXT_STRINGS) AS activities
FROM memberships m JOIN seasons s ON s.id=m.season_id JOIN membership_types t ON t.id=m.membership_type_id
WHERE m.person_id=sqlc.arg(person_id) ORDER BY s.starts_at DESC,m.id DESC;
