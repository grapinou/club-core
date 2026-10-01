-- name: ListMembershipTypeGroups :many
SELECT membership_type_id,group_id FROM membership_type_groups ORDER BY membership_type_id,group_id;

-- name: MembershipGroupCompatible :one
SELECT EXISTS(SELECT 1 FROM memberships m JOIN membership_type_groups c ON c.membership_type_id=m.membership_type_id
WHERE m.id=sqlc.arg(membership_id) AND c.group_id=sqlc.arg(group_id));

-- name: UpdateSelfServiceProfile :exec
UPDATE persons SET first_name=sqlc.arg(first_name),last_name=sqlc.arg(last_name),
phone_number=sqlc.arg(phone_number),address=sqlc.arg(address),updated_at=strftime('%Y-%m-%d %H:%M:%f','now')
WHERE id=sqlc.arg(id) AND archived_at IS NULL;
