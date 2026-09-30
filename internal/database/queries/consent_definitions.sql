-- name: CreateConsentDefinition :one
INSERT INTO consent_definitions(code, version, title, description, is_active)
VALUES (sqlc.arg(code), sqlc.arg(version), sqlc.arg(title), sqlc.arg(description), sqlc.arg(is_active)) RETURNING *;

-- name: GetConsentDefinition :one
SELECT * FROM consent_definitions WHERE id = sqlc.arg(id);

-- name: ListConsentDefinitions :many
SELECT * FROM consent_definitions ORDER BY code, version;

-- name: ListActiveConsentDefinitions :many
SELECT * FROM consent_definitions WHERE is_active ORDER BY code, version;

-- name: DeactivateConsentDefinition :one
UPDATE consent_definitions SET is_active = FALSE WHERE id = sqlc.arg(id) RETURNING *;
