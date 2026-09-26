-- +goose Up
ALTER TABLE organizations ADD COLUMN max_trials_per_person_per_season INTEGER
 CHECK (max_trials_per_person_per_season > 0);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM organizations WHERE max_trials_per_person_per_season IS NOT NULL) THEN
  RAISE EXCEPTION 'clear trial quota before removing its configuration';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE organizations DROP COLUMN max_trials_per_person_per_season;
