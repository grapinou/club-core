package administration

import (
	"context"
	"database/sql"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

// Family mutations concern relationships only. Access is granted separately.
func (s *Service) SaveGuardian(ctx context.Context, child, guardian int32, relationship string, primary, add bool) error {
	switch relationship {
	case "mother", "father", "guardian", "other":
	default:
		return ErrInvalid
	}
	if child == guardian {
		return ErrInvalid
	}
	return s.mutate(ctx, authorization.PersonsWrite, func(tx *sql.Tx, q *dbsqlc.Queries) (string, string, int32, error) {
		_, err := q.LockAdministrativePerson(ctx, child)
		if err != nil {
			return "", "", 0, err
		}
		c, err := q.GetPersonByID(ctx, child)
		if err != nil {
			return "", "", 0, err
		}
		if !s.memberships.IsEligibleMinor(c.BirthDate) {
			return "", "", 0, ErrInvalid
		}
		if _, err = q.LockAdministrativePerson(ctx, guardian); err != nil {
			return "", "", 0, err
		}
		if !add {
			if _, err = q.LockGuardianRelation(ctx, dbsqlc.LockGuardianRelationParams{ChildPersonID: child, GuardianPersonID: guardian}); err != nil {
				return "", "", 0, err
			}
		}
		if primary {
			if _, err = tx.ExecContext(ctx, `UPDATE person_guardians SET is_primary_contact=false WHERE child_person_id=?1`, child); err != nil {
				return "", "", 0, err
			}
		}
		if add {
			_, err = q.CreatePersonGuardian(ctx, dbsqlc.CreatePersonGuardianParams{ChildPersonID: child, GuardianPersonID: guardian, RelationshipType: relationship, IsPrimaryContact: primary})
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE person_guardians SET relationship_type=?3,is_primary_contact=?4 WHERE child_person_id=?1 AND guardian_person_id=?2`, child, guardian, relationship, primary)
		}
		return "family_relation_saved", "person", child, err
	})
}
