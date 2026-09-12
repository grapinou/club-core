-- +goose Up
CREATE TABLE guardian_access_grants (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 child_person_id INTEGER NOT NULL REFERENCES persons(id),
 guardian_person_id INTEGER NOT NULL REFERENCES persons(id),
 granted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 granted_by_user_id INTEGER REFERENCES users(id),
 revoked_at TIMESTAMPTZ,
 revoked_by_user_id INTEGER REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CHECK (child_person_id <> guardian_person_id),
 CHECK (revoked_at IS NOT NULL OR revoked_by_user_id IS NULL),
 CHECK (revoked_at IS NULL OR revoked_at >= granted_at)
);
CREATE UNIQUE INDEX guardian_access_one_active_pair
 ON guardian_access_grants(child_person_id,guardian_person_id) WHERE revoked_at IS NULL;
CREATE INDEX guardian_access_guardian ON guardian_access_grants(guardian_person_id,child_person_id) WHERE revoked_at IS NULL;

-- Relation locking serializes insertion with removal. Historical grants reference
-- Persons, not the removable relationship row.
-- +goose StatementBegin
CREATE FUNCTION protect_guardian_access_grant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP IN ('DELETE','TRUNCATE') THEN
  RAISE EXCEPTION 'guardian access history cannot be deleted';
 ELSIF TG_OP = 'UPDATE' THEN
  IF OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL OR
     (to_jsonb(NEW) - 'revoked_at' - 'revoked_by_user_id') IS DISTINCT FROM
     (to_jsonb(OLD) - 'revoked_at' - 'revoked_by_user_id') THEN
   RAISE EXCEPTION 'guardian access history is immutable except initial revocation';
  END IF;
 ELSE
  PERFORM id FROM person_guardians WHERE child_person_id=NEW.child_person_id
   AND guardian_person_id=NEW.guardian_person_id FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'guardian relationship required'; END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER guardian_access_no_truncate BEFORE TRUNCATE ON guardian_access_grants
 FOR EACH STATEMENT EXECUTE FUNCTION protect_guardian_access_grant();
CREATE TRIGGER guardian_access_history BEFORE INSERT OR UPDATE OR DELETE ON guardian_access_grants
 FOR EACH ROW EXECUTE FUNCTION protect_guardian_access_grant();

-- +goose StatementBegin
CREATE FUNCTION protect_guardian_relation_access() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'UPDATE' AND NEW.child_person_id=OLD.child_person_id AND NEW.guardian_person_id=OLD.guardian_person_id THEN
  RETURN NEW;
 END IF;
 IF EXISTS (SELECT 1 FROM guardian_access_grants WHERE child_person_id=OLD.child_person_id
  AND guardian_person_id=OLD.guardian_person_id AND revoked_at IS NULL) THEN
  RAISE EXCEPTION 'revoke guardian access before removing relationship';
 END IF;
 IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER guardian_relation_access BEFORE DELETE OR UPDATE ON person_guardians
 FOR EACH ROW EXECUTE FUNCTION protect_guardian_relation_access();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM guardian_access_grants) THEN
  RAISE EXCEPTION 'guardian access history prevents rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER guardian_relation_access ON person_guardians;
DROP FUNCTION protect_guardian_relation_access();
DROP TABLE guardian_access_grants;
DROP FUNCTION protect_guardian_access_grant();
