-- +goose Up

ALTER TABLE persons
ADD COLUMN archived_at TIMESTAMPTZ;


-- +goose Down

ALTER TABLE persons
DROP COLUMN archived_at;