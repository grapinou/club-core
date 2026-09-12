-- name: LockAccountPerson :one
SELECT first_name,last_name FROM persons WHERE id=$1 FOR UPDATE;

-- name: CreateUserForPersonUsername :one
INSERT INTO users(person_id,username,is_active) VALUES ($1,$2,true)
ON CONFLICT (username) DO NOTHING RETURNING id;
