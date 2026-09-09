-- name: CreatePersonEmergencyContact :one
INSERT INTO person_emergency_contacts(person_id, contact_person_id, relationship_label, priority)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: ListPersonEmergencyContacts :many
SELECT e.id AS emergency_contact_id, e.contact_person_id, p.first_name, p.last_name,
       p.phone_number, p.email, e.relationship_label, e.priority
FROM person_emergency_contacts e JOIN persons p ON p.id = e.contact_person_id
WHERE e.person_id = $1 ORDER BY e.priority;

-- name: ListEmergencyContactForPersons :many
SELECT e.id AS emergency_contact_id, e.person_id, e.contact_person_id, p.first_name, p.last_name,
       p.phone_number, p.email, e.relationship_label, e.priority
FROM person_emergency_contacts e JOIN persons p ON p.id = e.person_id
WHERE e.contact_person_id = $1 ORDER BY e.person_id;

-- name: UpdatePersonEmergencyContact :one
UPDATE person_emergency_contacts
SET relationship_label = $2, priority = $3, updated_at = clock_timestamp()
WHERE id = $1 RETURNING *;

-- name: DeletePersonEmergencyContact :exec
DELETE FROM person_emergency_contacts WHERE id = $1;
