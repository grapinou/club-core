// Package identityresolution stages declared identities before any durable Person
// is selected. A match is evidence for human review, never authentication.
package identityresolution

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrInvalidSubmission = errors.New("invalid submission")
	ErrUnavailable       = errors.New("submission unavailable")
	ErrClosed            = errors.New("submission already closed")
)

type SubmissionInput = dbsqlc.CreateRegistrationSubmissionParams

// Acceptance deliberately has no identifier, candidate count or diagnostic.
// Every successfully persisted submission returns exactly the same response.
type Acceptance struct {
	Status string `json:"status"`
}
type Submitter struct {
	db    *pgxpool.Pool
	email *EmailService
}

// NewEmailSubmitter atomically stages an identity and an optional delivery intent.
// It has no mailer dependency and cannot perform SMTP I/O.
func NewEmailSubmitter(db *pgxpool.Pool, email *EmailService) *Submitter {
	return &Submitter{db: db, email: email}
}

// NewSubmitter stages only for internal review workflows.
func NewSubmitter(db *pgxpool.Pool) *Submitter { return &Submitter{db: db} }

func validInput(in SubmissionInput) bool {
	for _, name := range []string{in.FirstName, in.LastName} {
		if strings.TrimSpace(name) == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 200 || strings.ContainsRune(name, 0) {
			return false
		}
	}
	for _, field := range []struct {
		value pgtype.Text
		max   int
	}{{in.Email, 254}, {in.PhoneNumber, 80}, {in.Address, 2000}} {
		if field.value.Valid && (!utf8.ValidString(field.value.String) || utf8.RuneCountInString(field.value.String) > field.max || strings.ContainsRune(field.value.String, 0)) {
			return false
		}
	}
	return !in.BirthDate.Valid || in.BirthDate.InfinityModifier == pgtype.Finite
}
func (s *Submitter) CreateSubmission(ctx context.Context, in SubmissionInput) (Acceptance, error) {
	if !validInput(in) {
		return Acceptance{}, ErrInvalidSubmission
	}
	_, err := s.create(ctx, in)
	if err != nil {
		return Acceptance{}, ErrUnavailable
	}
	return Acceptance{Status: "submission accepted"}, nil
}
func (s *Submitter) create(ctx context.Context, in SubmissionInput) (int32, error) {
	isolation := pgx.RepeatableRead
	if s.email != nil {
		// A fresh snapshot after the recipient advisory lock must see the previous
		// enqueue's commit. Eligibility is re-read in this same transaction.
		isolation = pgx.ReadCommitted
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	sub, err := q.CreateRegistrationSubmission(ctx, in)
	if err != nil {
		return 0, err
	}
	persons, err := q.ListIdentityMatchingPersons(ctx)
	if err != nil {
		return 0, err
	}
	found := false
	for _, p := range persons {
		evidence, ok := match(in, p)
		if !ok {
			continue
		}
		evidence.SubmissionID = sub.ID
		if err = q.CreateRegistrationCandidate(ctx, evidence); err != nil {
			return 0, err
		}
		found = true
	}
	if found {
		if err = q.MarkRegistrationForReview(ctx, sub.ID); err != nil {
			return 0, err
		}
	}
	if s.email != nil {
		if err = s.email.Enqueue(ctx, tx, sub); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return sub.ID, nil
}
func normalizedName(s string) string {
	return norm.NFC.String(strings.ToLower(strings.Join(strings.Fields(s), " ")))
}
func normalizedEmail(s pgtype.Text) string {
	if !s.Valid {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(s.String))
}
func normalizedPhone(s pgtype.Text) string {
	if !s.Valid {
		return ""
	}
	var b strings.Builder
	for i, r := range strings.TrimSpace(s.String) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\u00a0' || r == '-' || r == '.' || r == '(' || r == ')':
		default:
			return ""
		}
	}
	n := b.String()
	if strings.HasPrefix(n, "00") {
		n = n[2:]
	}
	if len(n) == 10 && n[0] == '0' {
		n = "33" + n[1:]
	}
	if len(n) < 7 || len(n) > 15 {
		return ""
	}
	return n
}
func match(in SubmissionInput, p dbsqlc.ListIdentityMatchingPersonsRow) (dbsqlc.CreateRegistrationCandidateParams, bool) {
	e := dbsqlc.CreateRegistrationCandidateParams{PersonID: p.ID}
	e.MatchedName = normalizedName(in.FirstName) == normalizedName(p.FirstName) && normalizedName(in.LastName) == normalizedName(p.LastName)
	if !e.MatchedName {
		return e, false
	}
	e.MatchedBirthDate = in.BirthDate.Valid && p.BirthDate.Valid && in.BirthDate.InfinityModifier == pgtype.Finite && p.BirthDate.InfinityModifier == pgtype.Finite && in.BirthDate.Time.Format("2006-01-02") == p.BirthDate.Time.Format("2006-01-02")
	email, phone := normalizedEmail(in.Email), normalizedPhone(in.PhoneNumber)
	e.MatchedEmail = email != "" && email == normalizedEmail(p.Email)
	e.MatchedPhone = phone != "" && phone == normalizedPhone(p.PhoneNumber)
	e.Confidence = "weak"
	if e.MatchedBirthDate || e.MatchedEmail || e.MatchedPhone {
		e.Confidence = "possible"
	}
	if e.MatchedBirthDate && (e.MatchedEmail || e.MatchedPhone) {
		e.Confidence = "strong"
	}
	return e, true
}

type PermissionChecker interface {
	HasPermission(context.Context, int32, authorization.Permission) (bool, error)
}
type ReviewService struct {
	db          *pgxpool.Pool
	permissions PermissionChecker
}

func NewReviewService(db *pgxpool.Pool, p PermissionChecker) *ReviewService {
	return &ReviewService{db: db, permissions: p}
}
func (s *ReviewService) require(ctx context.Context, actor int32) error {
	allowed, err := s.permissions.HasPermission(ctx, actor, authorization.RegistrationsReview)
	if err != nil {
		return err
	}
	if !allowed {
		return authorization.ErrForbidden
	}
	u, err := dbsqlc.New(s.db).GetUserByID(ctx, actor)
	if err != nil {
		return err
	}
	if !u.IsActive || !u.ActivatedAt.Valid {
		return authorization.ErrForbidden
	}
	return nil
}
func (s *ReviewService) List(ctx context.Context, actor int32) ([]dbsqlc.ListRegistrationReviewsRow, error) {
	if err := s.require(ctx, actor); err != nil {
		return nil, err
	}
	if err := ExpirePendingVerifications(ctx, s.db); err != nil {
		return nil, err
	}
	return dbsqlc.New(s.db).ListRegistrationReviews(ctx)
}
func (s *ReviewService) CountOpen(ctx context.Context, actor int32) (int64, error) {
	if err := s.require(ctx, actor); err != nil {
		return 0, err
	}
	if err := ExpirePendingVerifications(ctx, s.db); err != nil {
		return 0, err
	}
	return dbsqlc.New(s.db).CountOpenRegistrationReviews(ctx)
}

type Details struct {
	Submission dbsqlc.GetRegistrationSubmissionRow
	Candidates []dbsqlc.ListRegistrationCandidatesRow
}

func (s *ReviewService) GetDetails(ctx context.Context, actor, id int32) (Details, error) {
	if err := s.require(ctx, actor); err != nil {
		return Details{}, err
	}
	if err := ExpirePendingVerifications(ctx, s.db); err != nil {
		return Details{}, err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Details{}, err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	sub, err := q.GetRegistrationSubmission(ctx, id)
	if err != nil {
		return Details{}, err
	}
	candidates, err := q.ListRegistrationCandidates(ctx, id)
	if err != nil {
		return Details{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Details{}, err
	}
	return Details{sub, candidates}, nil
}
func (s *ReviewService) LinkPerson(ctx context.Context, actor, id, person int32) error {
	return s.resolve(ctx, actor, id, &person)
}
func (s *ReviewService) CreatePerson(ctx context.Context, actor, id int32) error {
	return s.resolve(ctx, actor, id, nil)
}
func cleanText(t pgtype.Text) pgtype.Text {
	v := strings.TrimSpace(t.String)
	return pgtype.Text{String: v, Valid: t.Valid && v != ""}
}
func (s *ReviewService) resolve(ctx context.Context, actor, id int32, person *int32) error {
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
	sub, err := q.LockRegistrationSubmission(ctx, id)
	if err != nil {
		return err
	}
	if !openStatus(sub.Status) {
		return ErrClosed
	}
	var target int32
	kind := "existing_person"
	if person != nil {
		if _, err = q.GetRegistrationCandidate(ctx, dbsqlc.GetRegistrationCandidateParams{SubmissionID: id, PersonID: *person}); err != nil {
			return err
		}
		target, err = q.LockRegistrationPerson(ctx, *person)
		if err != nil {
			return err
		}
	} else {
		kind = "new_person"
		p, createErr := q.CreatePerson(ctx, dbsqlc.CreatePersonParams{FirstName: strings.TrimSpace(sub.FirstName), LastName: strings.TrimSpace(sub.LastName), BirthDate: sub.BirthDate, Email: cleanText(sub.Email), PhoneNumber: cleanText(sub.PhoneNumber), Address: cleanText(sub.Address)})
		if createErr != nil {
			return createErr
		}
		target = p.ID
	}
	if err = invalidateProofs(ctx, tx, id); err != nil {
		return err
	}
	_, err = q.ResolveRegistrationSubmission(ctx, dbsqlc.ResolveRegistrationSubmissionParams{ID: id, ResolvedPersonID: pgtype.Int4{Int32: target, Valid: true}, ResolutionType: pgtype.Text{String: kind, Valid: true}, ResolvedByUserID: pgtype.Int4{Int32: actor, Valid: true}})
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
