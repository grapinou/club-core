// Package identitycorrections records proposals without changing reference identity.
// Review reuses the club's review and person-write permissions, never ownership.
package identitycorrections

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
	"github.com/grapinou/club-core/internal/identityresolution"
)

var ErrPending = errors.New("identity correction already pending")
var ErrClosed = errors.New("identity correction already reviewed")
var ErrChanged = errors.New("reference identity changed since proposal")

type Fields map[string]string

func (f Fields) Error() string { return "invalid identity correction" }

type Service struct {
	db  *sql.DB
	loc *time.Location
}

func New(db *sql.DB, loc *time.Location) *Service { return &Service{db, loc} }

func current(ctx context.Context, q *dbsqlc.Queries) (dbsqlc.GetPersonalAccountRow, error) {
	id, ok := auth.UserID(ctx)
	if !ok {
		return dbsqlc.GetPersonalAccountRow{}, sql.ErrNoRows
	}
	return q.GetPersonalAccount(ctx, id)
}
func (s *Service) Current(ctx context.Context) (dbsqlc.GetPersonalAccountRow, error) {
	return current(ctx, dbsqlc.New(s.db))
}

func (s *Service) validate(first, last, birth string) (string, string, dbtypes.Date, error) {
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)
	var date dbtypes.Date
	fields := Fields{}
	if birth != "" {
		t, err := time.Parse("2006-01-02", birth)
		now := time.Now().In(s.loc)
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if err != nil || t.After(today) {
			fields["birth_date"] = "Saisissez une date de naissance valide, qui ne soit pas dans le futur."
		} else {
			date = dbtypes.Date{Time: t, Valid: true}
		}
	}
	for _, key := range identityresolution.InvalidInputFields(identityresolution.SubmissionInput{FirstName: first, LastName: last, BirthDate: date}) {
		fields[key] = "Cette valeur est invalide ou trop longue."
	}
	if len(fields) > 0 {
		return "", "", date, fields
	}
	return first, last, date, nil
}
func equalDate(a, b dbtypes.Date) bool {
	return a.Valid == b.Valid && (!a.Valid || a.Time.Equal(b.Time))
}

func (s *Service) Request(ctx context.Context, first, last, birth string) error {
	first, last, date, err := s.validate(first, last, birth)
	if err != nil {
		return err
	}
	// database.New configures BEGIN IMMEDIATE: duplicate proposals and reviews
	// cannot validate the same snapshot concurrently, including across processes.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := dbsqlc.New(tx)
	a, err := current(ctx, q)
	if err != nil {
		return err
	}
	if a.IdentityCorrectionPending {
		return ErrPending
	}
	if first == a.FirstName && last == a.LastName && equalDate(date, a.BirthDate) {
		return Fields{"first_name": "Proposez au moins une correction de votre identité."}
	}
	actor, _ := auth.UserID(ctx)
	_, err = q.CreateIdentityCorrection(ctx, dbsqlc.CreateIdentityCorrectionParams{PersonID: a.PersonID, RequestingUserID: actor, OriginalFirstName: a.FirstName, OriginalLastName: a.LastName, OriginalBirthDate: a.BirthDate, ProposedFirstName: first, ProposedLastName: last, ProposedBirthDate: date})
	if err != nil {
		return err
	}
	return tx.Commit()
}
func reviewActor(ctx context.Context, q *dbsqlc.Queries) (int32, error) {
	id, ok := auth.UserID(ctx)
	if !ok {
		return 0, authorization.ErrForbidden
	}
	u, err := q.GetUserByID(ctx, id)
	if err != nil || !u.IsActive || !u.ActivatedAt.Valid || !u.PasswordHash.Valid {
		return 0, authorization.ErrForbidden
	}
	permissions, err := authorization.New(q).Permissions(ctx, id)
	if err != nil {
		return 0, err
	}
	if !permissions.Has(authorization.RegistrationsReview) || !permissions.Has(authorization.PersonsWrite) {
		return 0, authorization.ErrForbidden
	}
	return id, nil
}
func (s *Service) List(ctx context.Context) ([]dbsqlc.ListIdentityCorrectionsRow, error) {
	q := dbsqlc.New(s.db)
	if _, err := reviewActor(ctx, q); err != nil {
		return nil, err
	}
	return q.ListIdentityCorrections(ctx)
}
func (s *Service) Get(ctx context.Context, id int32) (dbsqlc.GetIdentityCorrectionRow, error) {
	q := dbsqlc.New(s.db)
	if _, err := reviewActor(ctx, q); err != nil {
		return dbsqlc.GetIdentityCorrectionRow{}, err
	}
	return q.GetIdentityCorrection(ctx, id)
}
func (s *Service) Review(ctx context.Context, id int32, decision string) error {
	if decision != "approved" && decision != "rejected" {
		return Fields{"decision": "Choisissez Valider ou Refuser."}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := dbsqlc.New(tx)
	actor, err := reviewActor(ctx, q)
	if err != nil {
		return err
	}
	r, err := q.GetIdentityCorrection(ctx, id)
	if err != nil {
		return err
	}
	if r.Status != "pending" {
		return ErrClosed
	}
	if decision == "approved" {
		if r.ArchivedAt.Valid || r.FirstName != r.OriginalFirstName || r.LastName != r.OriginalLastName || !equalDate(r.BirthDate, r.OriginalBirthDate) {
			return ErrChanged
		}
		// Revalidate before approval, preserving current name/date rules.
		birth := ""
		if r.ProposedBirthDate.Valid {
			birth = r.ProposedBirthDate.Time.Format("2006-01-02")
		}
		if _, _, _, err = s.validate(r.ProposedFirstName, r.ProposedLastName, birth); err != nil {
			return err
		}
	}
	n, err := q.ReviewIdentityCorrection(ctx, dbsqlc.ReviewIdentityCorrectionParams{ID: id, Status: decision, ReviewedByUserID: sql.NullInt32{Int32: actor, Valid: true}})
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrClosed
	}
	action := "person_identity_correction_rejected"
	if decision == "approved" {
		n, err = q.ApplyIdentityCorrection(ctx, dbsqlc.ApplyIdentityCorrectionParams{ID: r.PersonID, FirstName: r.ProposedFirstName, LastName: r.ProposedLastName, BirthDate: r.ProposedBirthDate})
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrChanged
		}
		action = "person_identity_corrected"
	}
	if err = q.CreateAdministrativeEvent(ctx, dbsqlc.CreateAdministrativeEventParams{ActorUserID: actor, Action: action, ResourceType: "person", ResourceID: r.PersonID}); err != nil {
		return err
	}
	return tx.Commit()
}
