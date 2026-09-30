package registrationapplications

import (
	"context"
	"errors"
	"time"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/memberships"
	"modernc.org/sqlite"
)

// FamilyIdentity is loaded exclusively from the session and effective access.
// Browser-supplied guardian identities never enter the authenticated workflow.
type FamilyIdentity struct {
	Guardian     identityresolution.SubmissionInput
	Child        *identityresolution.SubmissionInput
	Relationship string
}

func (s *Service) FamilyIdentity(ctx context.Context, child int32) (FamilyIdentity, error) {
	var f FamilyIdentity
	if s.guardians == nil {
		return f, authorization.ErrForbidden
	}
	children, err := s.guardians.ListManagedChildren(ctx)
	if err != nil {
		return f, err
	}
	if len(children) == 0 {
		return f, authorization.ErrForbidden
	}
	if child != 0 {
		found := false
		for _, c := range children {
			if c.PersonID == child {
				found = true
				f.Relationship = c.Relationship
			}
		}
		if !found {
			return f, authorization.ErrForbidden
		}
	}
	actor, _ := auth.UserID(ctx)
	q := dbsqlc.New(s.db)
	p, err := q.GetPersonalAccount(ctx, actor)
	if err != nil {
		return f, err
	}
	f.Guardian = identityresolution.SubmissionInput{FirstName: p.FirstName, LastName: p.LastName, BirthDate: p.BirthDate, Email: p.Email, PhoneNumber: p.PhoneNumber, Address: p.Address}
	if child != 0 {
		c, e := q.GetPersonByID(ctx, child)
		if e != nil {
			return f, e
		}
		f.Child = &identityresolution.SubmissionInput{FirstName: c.FirstName, LastName: c.LastName, BirthDate: c.BirthDate, Email: c.Email, PhoneNumber: c.PhoneNumber, Address: c.Address}
	}
	return f, nil
}
func (s *Service) familyInput(ctx context.Context, in Input, child int32) (Input, error) {
	f, err := s.FamilyIdentity(ctx, child)
	if err != nil {
		return in, err
	}
	if in.Child == nil {
		return in, authorization.ErrForbidden
	}
	// The staged guardian claim requires a contact email. It is corrected on
	// the account, never re-entered as an alternative guardian identity.
	if child == 0 && !validEmail(f.Guardian.Email, true) {
		return in, ValidationErrors{"guardian_email": "Complétez l’adresse email de votre compte avant d’inscrire un autre enfant."}
	}
	c := *in.Child
	c.Guardian = f.Guardian
	in.Child = &c
	if f.Child != nil {
		in.Identity = *f.Child
		in.Child.RelationshipType = f.Relationship
	}
	return in, nil
}
func (s *Service) ValidateFamily(ctx context.Context, in Input, csrf string, child int32) error {
	in, err := s.familyInput(ctx, in, child)
	if err != nil {
		return err
	}
	_, err = s.validate(ctx, dbsqlc.New(s.db), in, csrf, true)
	return err
}
func (s *Service) SubmitFamily(ctx context.Context, in Input, csrf string) (identityresolution.Acceptance, error) {
	in, err := s.familyInput(ctx, in, 0)
	if err != nil {
		return identityresolution.Acceptance{}, err
	}
	return s.submit(ctx, in, csrf, true)
}

// An already accessible child needs no identity matching or new family grant.
// The existing membership service remains responsible for choices, consents and
// the unique Person/season constraint. No approval takes place here.
func (s *Service) SubmitManagedChild(ctx context.Context, in Input, csrf string, child int32) (int32, error) {
	in, err := s.familyInput(ctx, in, child)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	guardian, err := s.guardians.AuthorizeManagedChildTx(ctx, tx, child)
	if err != nil {
		return 0, err
	}
	// Re-read after locking the authorized Persons; stale/forged child fields cannot win.
	q := dbsqlc.New(tx)
	c, err := q.GetPersonByID(ctx, child)
	if err != nil {
		return 0, err
	}
	in.Identity = identityresolution.SubmissionInput{FirstName: c.FirstName, LastName: c.LastName, BirthDate: c.BirthDate, Email: c.Email, PhoneNumber: c.PhoneNumber, Address: c.Address}
	p, err := s.validate(ctx, q, in, csrf, true)
	if err != nil {
		return 0, err
	}
	request := memberships.Request{PersonID: child, SeasonID: in.SeasonID, MembershipTypeID: in.MembershipTypeID, ActivityIDs: in.ActivityIDs}
	presented := []memberships.PresentedConsent{}
	for _, d := range in.Consents {
		request.Consents = append(request.Consents, memberships.Decision{ConsentDefinitionID: d.ConsentDefinitionID, Decision: d.Decision, GivenByPersonID: guardian})
		presented = append(presented, memberships.PresentedConsent{ConsentDefinitionID: d.ConsentDefinitionID, PresentedAt: dbtypes.Timestamp{Time: time.Unix(p.At, 0), Valid: true}})
	}
	m, err := s.memberships.CreateRequestWithPresentedConsentsTx(ctx, tx, request, presented)
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == 2067 {
			return 0, ValidationErrors{"season_id": "Un dossier existe déjà pour cet enfant et cette saison. Retrouvez-le dans son espace familial."}
		}
		return 0, err
	}
	return m.ID, tx.Commit()
}
