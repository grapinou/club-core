-- name: CreatePerson :one
INSERT INTO persons (
    first_name,
    last_name,
    birth_date,
    phone_number,
    email,
    address,
    notes
) VALUES (
    sqlc.arg(first_name),
    sqlc.arg(last_name),
    sqlc.arg(birth_date),
    sqlc.arg(phone_number),
    sqlc.arg(email),
    sqlc.arg(address),
    sqlc.arg(notes)
)
RETURNING *;

-- name: GetPersonByID :one
SELECT *
FROM persons
WHERE id = sqlc.arg(id);

-- name: ListPersons :many
SELECT *
FROM persons
WHERE archived_at IS NULL
ORDER BY last_name, first_name;

-- name: UpdatePerson :one
UPDATE persons
SET 
    first_name = sqlc.arg(first_name),
    last_name = sqlc.arg(last_name),
    birth_date = sqlc.arg(birth_date),
    phone_number = sqlc.arg(phone_number),
    email = sqlc.arg(email),
    address = sqlc.arg(address),
    -- Existing callers preserve notes unless explicitly requested (NULL clears them).
    notes = CASE WHEN CAST(sqlc.arg(update_notes) AS BOOLEAN) THEN sqlc.narg(notes) ELSE notes END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ArchivePerson :exec
UPDATE persons
SET archived_at = strftime('%Y-%m-%d %H:%M:%f','now')
WHERE id = sqlc.arg(id);

-- name: RestorePerson :exec
UPDATE persons
SET archived_at = NULL
WHERE id = sqlc.arg(id);

-- name: ListArchivedPersons :many
SELECT *
FROM persons
WHERE archived_at IS NOT NULL
ORDER BY last_name, first_name;
