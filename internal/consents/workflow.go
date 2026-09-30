package consents

import (
	"context"
	"time"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/civildate"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"github.com/jackc/pgx/v5/pgtype"
)

type PermissionChecker interface {
	HasPermission(context.Context, int32, authorization.Permission) (bool, error)
}

// RecordForActor adds web-resource authorization around the existing append-only
// writer. Family relation, grant, Persons and activated account stay locked until
// commit, so a revoked access cannot authorize a later decision.
func (s *Service) RecordForActor(ctx context.Context, p dbsqlc.CreateMembershipConsentParams, office bool, permissions PermissionChecker, loc *time.Location) error {
	actor, ok := auth.UserID(ctx)
	if !ok {
		return authorization.ErrForbidden
	}
	if office {
		allowed, err := permissions.HasPermission(ctx, actor, authorization.MembershipsApprove)
		if err != nil {
			return err
		}
		if !allowed {
			return authorization.ErrForbidden
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var person, viewer int32
	if err = tx.QueryRow(ctx, "SELECT person_id FROM memberships WHERE id=$1", p.MembershipID).Scan(&person); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, "SELECT person_id FROM users WHERE id=$1", actor).Scan(&viewer); err != nil {
		return err
	}
	if !office {
		p.GivenByPersonID = viewer
	}
	rows, err := tx.Query(ctx, "SELECT id FROM persons WHERE id=ANY($1::integer[]) ORDER BY id FOR UPDATE", []int32{person, viewer, p.GivenByPersonID})
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var active bool
	err = tx.QueryRow(ctx, `SELECT u.is_active AND u.activated_at IS NOT NULL AND u.password_hash IS NOT NULL AND p.archived_at IS NULL FROM users u JOIN persons p ON p.id=u.person_id WHERE u.id=$1 FOR SHARE OF u`, actor).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return authorization.ErrForbidden
	}
	var birth pgtype.Date
	err = tx.QueryRow(ctx, "SELECT birth_date FROM persons WHERE id=$1 AND archived_at IS NULL", person).Scan(&birth)
	if err != nil {
		return err
	}
	if !birth.Valid {
		return ErrUnauthorizedGiver
	}
	minor := civildate.IsMinor(birth.Time, time.Now().In(loc))
	if minor {
		if p.GivenByPersonID == person {
			return ErrUnauthorizedGiver
		}
		if !office {
			giver, err := guardianaccess.New(s.db, permissions, loc).AuthorizeManagedChildTx(ctx, tx, person)
			if err != nil || giver != viewer {
				return authorization.ErrForbidden
			}
		} else {
			var relation int32
			err = tx.QueryRow(ctx, "SELECT r.id FROM person_guardians r JOIN persons p ON p.id=r.guardian_person_id AND p.archived_at IS NULL WHERE child_person_id=$1 AND guardian_person_id=$2 FOR SHARE OF r", person, p.GivenByPersonID).Scan(&relation)
			if err != nil {
				return ErrUnauthorizedGiver
			}
		}
	} else if p.GivenByPersonID != person {
		return ErrUnauthorizedGiver
	}
	var presented bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM membership_consent_requirements WHERE membership_id=$1 AND consent_definition_id=$2)", p.MembershipID, p.ConsentDefinitionID).Scan(&presented)
	if err != nil {
		return err
	}
	if !presented {
		return ErrInactiveDefinition
	}
	if _, err = recordTx(ctx, tx, p); err != nil {
		return err
	}
	if office {
		err = dbsqlc.New(tx).CreateAdministrativeEvent(ctx, dbsqlc.CreateAdministrativeEventParams{ActorUserID: actor, Action: "membership_consent_recorded", ResourceType: "membership", ResourceID: p.MembershipID})
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
