-- name: CreatePersonEmergencyContact :one
INSERT INTO person_emergency_contacts(person_id, contact_person_id, relationship_label, priority)
VALUES (sqlc.arg(person_id), sqlc.arg(contact_person_id), sqlc.arg(relationship_label), sqlc.arg(priority)) RETURNING *;

-- name: ListPersonEmergencyContacts :many
SELECT e.id AS emergency_contact_id, e.contact_person_id, p.first_name, p.last_name,
       p.phone_number, p.email, e.relationship_label, e.priority
FROM person_emergency_contacts e JOIN persons p ON p.id = e.contact_person_id
WHERE e.person_id = sqlc.arg(person_id) ORDER BY e.priority;

-- name: ListEmergencyContactForPersons :many
SELECT e.id AS emergency_contact_id, e.person_id, e.contact_person_id, p.first_name, p.last_name,
       p.phone_number, p.email, e.relationship_label, e.priority
FROM person_emergency_contacts e JOIN persons p ON p.id = e.person_id
WHERE e.contact_person_id = sqlc.arg(contact_person_id) ORDER BY e.person_id;

-- name: UpdatePersonEmergencyContact :one
UPDATE person_emergency_contacts
SET relationship_label = sqlc.arg(relationship_label), priority = sqlc.arg(priority), updated_at = strftime('%Y-%m-%d %H:%M:%f','now')
WHERE id = sqlc.arg(id) RETURNING *;

-- name: DeletePersonEmergencyContact :exec
DELETE FROM person_emergency_contacts WHERE id = sqlc.arg(id);
