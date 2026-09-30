-- name: LockGuardianRelation :one
SELECT * FROM person_guardians WHERE child_person_id=sqlc.arg(child_person_id) AND guardian_person_id=sqlc.arg(guardian_person_id);

-- name: CreateGuardianAccessGrant :one
INSERT INTO guardian_access_grants(child_person_id,guardian_person_id,granted_by_user_id)
VALUES (sqlc.arg(child_person_id),sqlc.arg(guardian_person_id),sqlc.arg(granted_by_user_id))
ON CONFLICT (child_person_id,guardian_person_id) WHERE revoked_at IS NULL
DO NOTHING RETURNING *;

-- name: GetActiveGuardianAccess :one
SELECT * FROM guardian_access_grants WHERE child_person_id=sqlc.arg(child_person_id) AND guardian_person_id=sqlc.arg(guardian_person_id) AND revoked_at IS NULL;

-- name: RevokeGuardianAccessGrant :exec
UPDATE guardian_access_grants SET revoked_at=strftime('%Y-%m-%d %H:%M:%f','now'),revoked_by_user_id=sqlc.arg(revoked_by_user_id)
WHERE child_person_id=sqlc.arg(child_person_id) AND guardian_person_id=sqlc.arg(guardian_person_id) AND revoked_at IS NULL;

-- name: ListActiveGuardiansForChild :many
SELECT p.id,p.first_name,p.last_name,r.relationship_type,g.granted_at,c.birth_date
FROM guardian_access_grants g
JOIN person_guardians r USING(child_person_id,guardian_person_id)
JOIN persons p ON p.id=g.guardian_person_id AND p.archived_at IS NULL
JOIN persons c ON c.id=g.child_person_id AND c.archived_at IS NULL
JOIN users u ON u.person_id=p.id AND u.is_active AND u.activated_at IS NOT NULL AND u.password_hash IS NOT NULL
WHERE g.child_person_id=sqlc.arg(child_person_id) AND g.revoked_at IS NULL ORDER BY g.id;

-- name: ListManagedChildrenForGuardian :many
SELECT c.id,c.first_name,c.last_name,c.birth_date,r.relationship_type,g.granted_at
FROM guardian_access_grants g
JOIN person_guardians r USING(child_person_id,guardian_person_id)
JOIN persons p ON p.id=g.guardian_person_id AND p.archived_at IS NULL
JOIN persons c ON c.id=g.child_person_id AND c.archived_at IS NULL
JOIN users u ON u.person_id=p.id AND u.is_active AND u.activated_at IS NOT NULL AND u.password_hash IS NOT NULL
WHERE u.id=sqlc.arg(id) AND g.revoked_at IS NULL ORDER BY g.id;

-- name: GetGuardianAccessFacts :one
SELECT c.birth_date FROM guardian_access_grants g
JOIN person_guardians r USING(child_person_id,guardian_person_id)
JOIN persons p ON p.id=g.guardian_person_id AND p.archived_at IS NULL
JOIN persons c ON c.id=g.child_person_id AND c.archived_at IS NULL
WHERE g.child_person_id=sqlc.arg(child_person_id) AND g.guardian_person_id=sqlc.arg(guardian_person_id) AND g.revoked_at IS NULL;

-- name: ListInteractionParticipants :many
SELECT u.id AS user_id,p.id AS person_id,p.birth_date
FROM users u JOIN persons p ON p.id=u.person_id
WHERE u.id IN (sqlc.slice(ids)) AND u.is_active AND u.activated_at IS NOT NULL
AND u.password_hash IS NOT NULL AND p.archived_at IS NULL;

-- name: ListInteractionGuardianEdges :many
SELECT g.child_person_id,g.guardian_person_id FROM guardian_access_grants g
JOIN person_guardians r USING(child_person_id,guardian_person_id)
WHERE g.revoked_at IS NULL AND g.child_person_id IN (sqlc.slice(ids))
AND g.guardian_person_id IN (sqlc.slice(guardian_ids));
