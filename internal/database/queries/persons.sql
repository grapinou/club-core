-- name: CreatePerson :one
INSERT INTO persons (
    first_name,
    last_name,
    birth_date,
    phone_number,
    email,
    address
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6
)
RETURNING *;

-- name: GetPersonByID :one
SELECT *
FROM persons
WHERE id = $1;

-- name: ListPersons :many
SELECT *
FROM persons
WHERE archived_at IS NULL
ORDER BY last_name, first_name;

-- name: UpdatePerson :one
UPDATE persons
SET 
    first_name = $2,
    last_name = $3,
    birth_date = $4,
    phone_number = $5,
    email = $6,
    address = $7
WHERE id = $1
RETURNING *;

-- name: ArchivePerson :exec
UPDATE persons
SET archived_at = NOW()
WHERE id = $1;

-- name: RestorePerson :exec
UPDATE persons
SET archived_at = NULL
WHERE id = $1;

-- name: ListArchivedPersons :many
SELECT *
FROM persons
WHERE archived_at IS NOT NULL
ORDER BY last_name, first_name;
