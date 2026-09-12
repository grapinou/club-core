-- Read-only dashboard projections: no administrative dossier or credentials.
-- name: GetDashboardPerson :one
SELECT p.id,p.first_name,p.last_name FROM persons p JOIN users u ON u.person_id=p.id WHERE u.id=$1;

-- name: ListDashboardMemberships :many
SELECT m.id,m.status,s.name AS season_name,t.name AS membership_type_name,
 ARRAY(SELECT a.name FROM activities a JOIN membership_activities ma ON ma.activity_id=a.id WHERE ma.membership_id=m.id ORDER BY a.name)::text[] AS activities
FROM memberships m JOIN seasons s ON s.id=m.season_id JOIN membership_types t ON t.id=m.membership_type_id
WHERE m.person_id=$1 ORDER BY s.starts_at DESC,m.id DESC;
