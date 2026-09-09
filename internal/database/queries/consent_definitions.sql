-- name: CreateConsentDefinition :one
INSERT INTO consent_definitions(code, version, title, description, is_active)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: GetConsentDefinition :one
SELECT * FROM consent_definitions WHERE id = $1;

-- name: ListConsentDefinitions :many
SELECT * FROM consent_definitions ORDER BY code, version;

-- name: ListActiveConsentDefinitions :many
SELECT * FROM consent_definitions WHERE is_active ORDER BY code, version;

-- name: DeactivateConsentDefinition :one
UPDATE consent_definitions SET is_active = FALSE WHERE id = $1 RETURNING *;
