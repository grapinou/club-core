-- name: ListMembershipTypeGroups :many
SELECT membership_type_id,group_id FROM membership_type_groups ORDER BY membership_type_id,group_id;

-- name: MembershipGroupCompatible :one
SELECT EXISTS(SELECT 1 FROM memberships m JOIN membership_type_groups c ON c.membership_type_id=m.membership_type_id
WHERE m.id=sqlc.arg(membership_id) AND c.group_id=sqlc.arg(group_id));
