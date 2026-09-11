-- +goose Up
INSERT INTO roles(name) VALUES ('president'), ('secretary'), ('treasurer'), ('coach')
ON CONFLICT (name) DO NOTHING;

-- +goose Down
-- Roles may predate this migration or be assigned since its application.
-- Retain these reference rows and all assignments rather than deleting evidence.
SELECT 1;
