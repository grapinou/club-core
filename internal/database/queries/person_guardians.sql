-- name: CreatePersonGuardian :one
INSERT INTO person_guardians (child_person_id, guardian_person_id, relationship_type, is_primary_contact)
VALUES (sqlc.arg(child_person_id), sqlc.arg(guardian_person_id), sqlc.arg(relationship_type), CAST(sqlc.arg(is_primary_contact) AS BOOLEAN))
RETURNING *;

-- name: ListPersonGuardians :many
SELECT pg.*, p.first_name, p.last_name, p.phone_number, p.email
FROM person_guardians pg
JOIN persons p ON p.id = pg.guardian_person_id
WHERE pg.child_person_id = sqlc.arg(child_person_id)
ORDER BY pg.id;

-- name: ListGuardianChildren :many
SELECT pg.*, p.first_name, p.last_name
FROM person_guardians pg
JOIN persons p ON p.id = pg.child_person_id
WHERE pg.guardian_person_id = sqlc.arg(guardian_person_id)
ORDER BY pg.id;

-- name: UpdatePersonGuardian :one
UPDATE person_guardians
SET relationship_type = sqlc.arg(relationship_type), is_primary_contact = CAST(sqlc.arg(is_primary_contact) AS BOOLEAN)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeletePersonGuardian :exec
DELETE FROM person_guardians WHERE id = sqlc.arg(id);
