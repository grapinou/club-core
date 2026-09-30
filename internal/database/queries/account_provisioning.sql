-- name: LockAccountPerson :one
SELECT first_name,last_name FROM persons WHERE id=sqlc.arg(id);

-- name: CreateUserForPersonUsername :one
INSERT INTO users(person_id,username,is_active) VALUES (sqlc.arg(person_id),sqlc.arg(username),true)
ON CONFLICT (username) DO NOTHING RETURNING id;
