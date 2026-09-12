package identityresolution

import (
	"context"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// CreateGuardianClaim stages evidence with the same deterministic matcher as
// submissions. Even a single strong candidate requires administrative resolution.
func CreateGuardianClaim(ctx context.Context, tx pgx.Tx, in SubmissionInput) (dbsqlc.GuardianIdentityClaim, error) {
	q := dbsqlc.New(tx)
	claim, err := q.CreateGuardianIdentityClaim(ctx, dbsqlc.CreateGuardianIdentityClaimParams{FirstName: in.FirstName, LastName: in.LastName, BirthDate: in.BirthDate, Email: in.Email.String, PhoneNumber: in.PhoneNumber, Address: in.Address})
	if err != nil {
		return claim, err
	}
	persons, err := q.ListIdentityMatchingPersons(ctx)
	if err != nil {
		return claim, err
	}
	found := false
	for _, p := range persons {
		e, ok := match(in, p)
		if !ok {
			continue
		}
		found = true
		err = q.CreateGuardianIdentityCandidate(ctx, dbsqlc.CreateGuardianIdentityCandidateParams{GuardianClaimID: claim.ID, PersonID: e.PersonID, Confidence: e.Confidence, MatchedName: e.MatchedName, MatchedBirthDate: e.MatchedBirthDate, MatchedEmail: e.MatchedEmail, MatchedPhone: e.MatchedPhone})
		if err != nil {
			return claim, err
		}
	}
	if found {
		err = q.MarkGuardianIdentityReview(ctx, claim.ID)
	} else {
		err = resolveGuardianClaim(ctx, tx, claim, 0, nil)
	}
	if err != nil {
		return claim, err
	}
	return q.GetGuardianIdentityClaim(ctx, claim.ID)
}
func resolveGuardianClaim(ctx context.Context, tx pgx.Tx, c dbsqlc.GuardianIdentityClaim, actor int32, person *int32) error {
	q := dbsqlc.New(tx)
	kind := "new_person"
	var target int32
	if person != nil {
		candidates, err := q.ListGuardianIdentityCandidates(ctx, c.ID)
		if err != nil {
			return err
		}
		found := false
		for _, v := range candidates {
			if v.PersonID == *person {
				found = true
			}
		}
		if !found {
			return ErrUnavailable
		}
		target = *person
		kind = "existing_person"
		if _, err = q.LockRegistrationPerson(ctx, target); err != nil {
			return err
		}
	} else {
		p, err := CreateDeclaredPerson(ctx, tx, dbsqlc.RegistrationSubmission{FirstName: c.FirstName, LastName: c.LastName, BirthDate: c.BirthDate, Email: pgtype.Text{String: c.Email, Valid: true}, PhoneNumber: c.PhoneNumber, Address: c.Address})
		if err != nil {
			return err
		}
		target = p.ID
	}
	return q.ResolveGuardianIdentityClaim(ctx, dbsqlc.ResolveGuardianIdentityClaimParams{ID: c.ID, ResolvedPersonID: pgtype.Int4{Int32: target, Valid: true}, ResolutionType: pgtype.Text{String: kind, Valid: true}, ResolvedByUserID: pgtype.Int4{Int32: actor, Valid: actor != 0}})
}

// ResolveGuardian uses the submission lock order shared with child resolution and
// confirmation; the candidate snapshot is the only allowed existing target set.
func (s *ReviewService) ResolveGuardian(ctx context.Context, actor, id int32, person *int32) error {
	if session, ok := auth.UserID(ctx); !ok || session != actor {
		return authorization.ErrForbidden
	}
	if err := s.require(ctx, actor); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = LockDeliveryDecision(ctx, tx, id); err != nil {
		return err
	}
	q := dbsqlc.New(tx)
	if _, err = q.LockRegistrationSubmission(ctx, id); err != nil {
		return err
	}
	a, err := q.LockRegistrationApplication(ctx, id)
	if err != nil {
		return err
	}
	child, err := q.GetChildRegistrationApplication(ctx, a.ID)
	if err != nil {
		return err
	}
	c, err := q.LockGuardianIdentityClaim(ctx, child.GuardianClaimID)
	if err != nil {
		return err
	}
	if c.Status == "resolved" || c.Status == "cancelled" {
		return ErrClosed
	}
	if err = resolveGuardianClaim(ctx, tx, c, actor, person); err != nil {
		return err
	}
	if s.finalizer != nil {
		if err = s.finalizer.FinalizeSubmission(ctx, tx, id); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.afterResolution(ctx, id)
	return nil
}

type ChildDetails struct {
	GuardianUser         *dbsqlc.User
	Application          dbsqlc.ChildRegistrationApplication
	Guardian             dbsqlc.GuardianIdentityClaim
	Candidates           []dbsqlc.ListGuardianIdentityCandidatesRow
	RelationExists       bool
	ExistingRelationship string
}

func (s *ReviewService) afterResolution(ctx context.Context, id int32) {
	if f, ok := s.finalizer.(interface{ AfterResolution(context.Context, int32) }); ok {
		f.AfterResolution(ctx, id)
	}
}
