-- name: ListCurrentPublicSeasons :many
SELECT id,name FROM seasons WHERE is_active AND sqlc.arg(today)::date BETWEEN starts_at AND ends_at
ORDER BY starts_at,id;

-- name: ListPublicSchedule :many
SELECT gs.weekday,to_char(gs.start_time,'HH24:MI')::text AS start_time,
 to_char(gs.end_time,'HH24:MI')::text AS end_time,a.name AS activity_name,
 CASE WHEN g.show_name_publicly THEN g.name ELSE '' END::text AS group_name,
 coalesce(gs.practice_label,'')::text AS practice_label,
 coalesce(l.name,gs.location,'')::text AS location_name,coalesce(l.address,'')::text AS location_address
FROM group_slots gs JOIN groups g ON g.id=gs.group_id JOIN activities a ON a.id=g.activity_id
JOIN seasons s ON s.id=gs.season_id LEFT JOIN locations l ON l.id=gs.location_id
WHERE gs.season_id=sqlc.arg(season_id) AND s.is_active AND g.is_active AND a.is_active AND gs.is_active
 AND gs.valid_from<=sqlc.arg(today)::date AND (gs.valid_until IS NULL OR gs.valid_until>=sqlc.arg(today)::date)
 AND (gs.location_id IS NULL OR (l.is_active AND l.organization_id=sqlc.arg(organization_id)))
ORDER BY gs.weekday,gs.start_time,gs.id;
