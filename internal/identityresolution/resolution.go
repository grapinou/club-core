package identityresolution

import (
	"context"
	"database/sql"
	"strings"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

// ResolutionFinalizer joins a public application's membership decision to the
// identity transaction. It does nothing for legacy identity-only submissions.
type ResolutionFinalizer interface {
	FinalizeSubmission(context.Context, *sql.Tx, int32) error
}

func (s *EmailService) SetFinalizer(f ResolutionFinalizer)  { s.finalizer = f }
func (s *ReviewService) SetFinalizer(f ResolutionFinalizer) { s.finalizer = f }

func CreateDeclaredPerson(ctx context.Context, tx *sql.Tx, sub dbsqlc.RegistrationSubmission) (dbsqlc.Person, error) {
	return dbsqlc.New(tx).CreatePerson(ctx, dbsqlc.CreatePersonParams{FirstName: strings.TrimSpace(sub.FirstName), LastName: strings.TrimSpace(sub.LastName), BirthDate: sub.BirthDate, Email: cleanText(sub.Email), PhoneNumber: cleanText(sub.PhoneNumber), Address: cleanText(sub.Address)})
}

// ResolveUnmatchedSelf retains the adult API and shares the zero-candidate
// primitive with the validated child application.
func ResolveUnmatchedSelf(ctx context.Context, tx *sql.Tx, id int32) error {
	return ResolveUnmatchedApplication(ctx, tx, id)
}

// ResolveUnmatchedApplication requires a complete, validated public application.
func ResolveUnmatchedApplication(ctx context.Context, tx *sql.Tx, id int32) error {
	q := dbsqlc.New(tx)
	sub, err := q.LockRegistrationSubmission(ctx, id)
	if err != nil {
		return err
	}
	if !openStatus(sub.Status) {
		return ErrClosed
	}
	candidates, err := q.ListRegistrationCandidates(ctx, id)
	if err != nil {
		return err
	}
	if len(candidates) != 0 {
		return ErrNotEligible
	}
	p, err := CreateDeclaredPerson(ctx, tx, sub)
	if err != nil {
		return err
	}
	_, err = q.ResolveRegistrationSubmission(ctx, dbsqlc.ResolveRegistrationSubmissionParams{ID: id, ResolvedPersonID: sql.NullInt32{Int32: p.ID, Valid: true}, ResolutionType: sql.NullString{String: "new_person", Valid: true}})
	return err
}
