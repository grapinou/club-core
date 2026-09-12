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

// InvalidInputFields shares identity size/encoding rules with the public form.
// Birth date and email remain optional for identity-only internal workflows.
func InvalidInputFields(in SubmissionInput) []string {
	var invalid []string
	for _, field := range []struct {
		key      string
		value    pgtype.Text
		max      int
		required bool
	}{
		{"first_name", pgtype.Text{String: in.FirstName, Valid: true}, 200, true},
		{"last_name", pgtype.Text{String: in.LastName, Valid: true}, 200, true},
		{"email", in.Email, 254, false}, {"phone_number", in.PhoneNumber, 80, false}, {"address", in.Address, 2000, false},
	} {
		if (field.required && strings.TrimSpace(field.value.String) == "") || (field.value.Valid && (!utf8.ValidString(field.value.String) || utf8.RuneCountInString(field.value.String) > field.max || strings.ContainsRune(field.value.String, 0))) {
			invalid = append(invalid, field.key)
		}
	}
	if in.BirthDate.Valid && in.BirthDate.InfinityModifier != pgtype.Finite {
		invalid = append(invalid, "birth_date")
	}
	return invalid
}
func ValidInput(in SubmissionInput) bool { return len(InvalidInputFields(in)) == 0 }
func (s *Submitter) CreateSubmission(ctx context.Context, in SubmissionInput) (Acceptance, error) {
	if !ValidInput(in) {
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
	sub, err := s.CreateInTransaction(ctx, tx, in)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return sub.ID, nil
}

// CreateInTransaction stages identity evidence and any email intent atomically
// with a complete public application. Callers use READ COMMITTED for the quota.
func (s *Submitter) CreateInTransaction(ctx context.Context, tx pgx.Tx, in SubmissionInput) (dbsqlc.RegistrationSubmission, error) {
	if !ValidInput(in) {
		return dbsqlc.RegistrationSubmission{}, ErrInvalidSubmission
	}
	q := dbsqlc.New(tx)
	sub, err := q.CreateRegistrationSubmission(ctx, in)
	if err != nil {
		return dbsqlc.RegistrationSubmission{}, err
	}
	persons, err := q.ListIdentityMatchingPersons(ctx)
	if err != nil {
		return dbsqlc.RegistrationSubmission{}, err
	}
	found := false
	for _, p := range persons {
		evidence, ok := match(in, p)
		if !ok {
			continue
		}
		evidence.SubmissionID = sub.ID
		if err = q.CreateRegistrationCandidate(ctx, evidence); err != nil {
			return dbsqlc.RegistrationSubmission{}, err
		}
		found = true
	}
	if found {
		if err = q.MarkRegistrationForReview(ctx, sub.ID); err != nil {
			return dbsqlc.RegistrationSubmission{}, err
		}
	}
	if s.email != nil {
		if err = s.email.Enqueue(ctx, tx, sub); err != nil {
			return dbsqlc.RegistrationSubmission{}, err
		}
	}
	return sub, nil
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
	finalizer   ResolutionFinalizer
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
	Child       *ChildDetails
	Submission  dbsqlc.GetRegistrationSubmissionRow
	Candidates  []dbsqlc.ListRegistrationCandidatesRow
	Application *dbsqlc.GetRegistrationApplicationDetailsRow
	Activities  []dbsqlc.Activity
	Consents    []dbsqlc.ListRegistrationApplicationConsentsRow
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
	detail := Details{Submission: sub, Candidates: candidates}
	application, appErr := q.GetRegistrationApplicationDetails(ctx, id)
	if appErr == nil {
		detail.Application = &application
		child, e := q.GetChildRegistrationApplication(ctx, application.ID)
		if e == nil {
			cd := &ChildDetails{Application: child}
			if cd.Guardian, err = q.GetGuardianIdentityClaim(ctx, child.GuardianClaimID); err != nil {
				return Details{}, err
			}
			if cd.Candidates, err = q.ListGuardianIdentityCandidates(ctx, child.GuardianClaimID); err != nil {
				return Details{}, err
			}
			e = tx.QueryRow(ctx, `SELECT relationship_type FROM person_guardians WHERE child_person_id=$1 AND guardian_person_id=$2`, sub.ResolvedPersonID, cd.Guardian.ResolvedPersonID).Scan(&cd.ExistingRelationship)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return Details{}, e
			}
			cd.RelationExists = e == nil
			if cd.Guardian.ResolvedPersonID.Valid {
				u, e := q.GetUserByPerson(ctx, cd.Guardian.ResolvedPersonID.Int32)
				if e == nil {
					cd.GuardianUser = &u
				} else if !errors.Is(e, pgx.ErrNoRows) {
					return Details{}, e
				}
			}
			detail.Child = cd
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return Details{}, e
		}
		if detail.Activities, err = q.ListRegistrationApplicationActivities(ctx, application.ID); err != nil {
			return Details{}, err
		}
		if detail.Consents, err = q.ListRegistrationApplicationConsents(ctx, application.ID); err != nil {
			return Details{}, err
		}
	} else if !errors.Is(appErr, pgx.ErrNoRows) {
		return Details{}, appErr
	}
	if err = tx.Commit(ctx); err != nil {
		return Details{}, err
	}
	return detail, nil
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
		p, createErr := CreateDeclaredPerson(ctx, tx, sub)
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

// RetryApplication permits a reviewer to retry the original immutable choices
// after restoring availability. Identity is never re-resolved here.
func (s *ReviewService) RetryApplication(ctx context.Context, actor, id int32) error {
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
	sub, err := dbsqlc.New(tx).LockRegistrationSubmission(ctx, id)
	if err != nil {
		return err
	}
	if sub.Status != "resolved" || s.finalizer == nil {
		return ErrUnavailable
	}
	if err = s.finalizer.FinalizeSubmission(ctx, tx, id); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.afterResolution(ctx, id)
	return nil
}
