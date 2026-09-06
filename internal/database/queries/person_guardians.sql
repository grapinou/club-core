-- name: CreatePersonGuardian :one
INSERT INTO person_guardians (child_person_id, guardian_person_id, relationship_type, is_primary_contact)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListPersonGuardians :many
SELECT pg.*, p.first_name, p.last_name, p.phone_number, p.email
FROM person_guardians pg
JOIN persons p ON p.id = pg.guardian_person_id
WHERE pg.child_person_id = $1
ORDER BY pg.id;

-- name: ListGuardianChildren :many
SELECT pg.*, p.first_name, p.last_name
FROM person_guardians pg
JOIN persons p ON p.id = pg.child_person_id
WHERE pg.guardian_person_id = $1
ORDER BY pg.id;

-- name: UpdatePersonGuardian :one
UPDATE person_guardians
SET relationship_type = $2, is_primary_contact = $3
WHERE id = $1
RETURNING *;

-- name: DeletePersonGuardian :exec
DELETE FROM person_guardians WHERE id = $1;
