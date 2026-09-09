-- Writes must go through internal/consents.Service for transverse validation.
-- name: CreateMembershipConsent :one
INSERT INTO membership_consents(membership_id, consent_definition_id, decision, given_by_person_id)
VALUES ($1, $2, $3, $4) RETURNING *;

-- Serialize decisions for a membership, and prevent its person changing during validation.
-- name: LockConsentMembership :one
SELECT person_id FROM memberships WHERE id = $1 FOR UPDATE;

-- Keep the authorizing relationship alive until the decision commits.
-- name: LockConsentGuardian :one
SELECT id FROM person_guardians
WHERE child_person_id = $1 AND guardian_person_id = $2 FOR SHARE;

-- Prevent activation changes between validation and insertion.
-- name: LockConsentDefinition :one
SELECT is_active FROM consent_definitions WHERE id = $1 FOR SHARE;

-- name: GetCurrentMembershipConsentDecision :one
SELECT decision FROM membership_consents
WHERE membership_id = $1 AND consent_definition_id = $2
ORDER BY recorded_at DESC, id DESC LIMIT 1;

-- name: ListCurrentMembershipConsents :many
SELECT m.id AS membership_id, d.id AS consent_definition_id, d.code, d.version,
       d.title, d.description, d.is_active AS definition_is_active,
       c.decision AS current_decision, c.recorded_at AS decision_recorded_at,
       p.id AS given_by_person_id, p.first_name AS given_by_first_name, p.last_name AS given_by_last_name
FROM memberships m CROSS JOIN consent_definitions d
LEFT JOIN membership_consents c ON c.id = (
    SELECT latest.id FROM membership_consents latest
    WHERE latest.membership_id = m.id AND latest.consent_definition_id = d.id
    ORDER BY latest.recorded_at DESC, latest.id DESC LIMIT 1
)
LEFT JOIN persons p ON p.id = c.given_by_person_id
WHERE m.id = $1 AND (d.is_active OR c.id IS NOT NULL)
ORDER BY d.code, d.version;

-- name: ListMembershipConsentHistory :many
SELECT c.*, d.code, d.version, d.title, d.description, d.is_active AS definition_is_active,
       p.first_name AS given_by_first_name, p.last_name AS given_by_last_name
FROM membership_consents c JOIN consent_definitions d ON d.id = c.consent_definition_id
JOIN persons p ON p.id = c.given_by_person_id
WHERE c.membership_id = $1 AND c.consent_definition_id = $2
ORDER BY c.recorded_at, c.id;

-- name: ListMembershipConsentsHistory :many
SELECT c.*, d.code, d.version, d.title, d.description, d.is_active AS definition_is_active,
       p.first_name AS given_by_first_name, p.last_name AS given_by_last_name
FROM membership_consents c JOIN consent_definitions d ON d.id = c.consent_definition_id
JOIN persons p ON p.id = c.given_by_person_id
WHERE c.membership_id = $1 ORDER BY c.recorded_at, c.id;
