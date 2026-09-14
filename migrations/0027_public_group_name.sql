-- +goose Up
-- Publishing a schedule does not imply publishing an internal grouping name.
ALTER TABLE groups ADD COLUMN show_name_publicly BOOLEAN NOT NULL DEFAULT TRUE;

-- +goose Down
ALTER TABLE groups DROP COLUMN show_name_publicly;
