// Package memberships owns submission, computed completeness and approval.
// Callers must use this service for workflow writes.
package memberships

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/grapinou/club-core/internal/accounts/provisioning"
	"github.com/grapinou/club-core/internal/activation"
	"github.com/grapinou/club-core/internal/civildate"
	"github.com/grapinou/club-core/internal/consents"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidRequest = errors.New("invalid membership request")
var ErrNotPending = errors.New("membership is not pending")

type IncompleteError struct{ Completeness Completeness }

func (e *IncompleteError) Error() string {
	return fmt.Sprintf("incomplete membership: %v", e.Completeness.BlockingIssues)
}

type Service struct {
	db         *pgxpool.Pool
	activation *activation.Service
	location   *time.Location
}

func New(db *pgxpool.Pool, validity time.Duration, location *time.Location) (*Service, error) {
	if location == nil {
		return nil, errors.New("administrative timezone is required")
	}
	a, err := activation.New(db, validity)
	if err != nil {
		return nil, err
	}
	return &Service{db: db, activation: a, location: location}, nil
}

type Decision struct {
	ConsentDefinitionID int32
	Decision            string
	GivenByPersonID     int32
}
type Request struct {
	PersonID, SeasonID, MembershipTypeID int32
	ActivityIDs                          []int32
	Consents                             []Decision
}
type Completeness struct {
	IsMinor                     *bool
	BlockingIssues              []string
	Warnings                    []string
	MissingConsentDefinitionIDs []int32
}
type AccountState struct {
	Exists          bool
	IsActive        bool
	IsActivated     bool
	NeedsActivation bool
}

type Details struct {
	Guardians           []dbsqlc.ListPersonGuardiansRow
	EmergencyContacts   []dbsqlc.ListPersonEmergencyContactsRow
	Account             AccountState
	Membership          dbsqlc.GetMembershipDetailsRow
	Activities          []dbsqlc.Activity
	ConsentRequirements []dbsqlc.ListMembershipConsentRequirementsRow
	Completeness        Completeness
}
type Approval struct {
	Membership         dbsqlc.Membership
	User               dbsqlc.User
	Completeness       Completeness
	ActivationDelivery *activation.Delivery
}

type PresentedConsent struct {
	ConsentDefinitionID int32
	PresentedAt         pgtype.Timestamptz
}

func (s *Service) IsAdult(birth pgtype.Date) bool {
	return birth.Valid && birth.InfinityModifier == pgtype.Finite && !IsMinor(birth.Time, time.Now().In(s.location))
}

func (s *Service) CreateRequest(ctx context.Context, r Request) (dbsqlc.Membership, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return dbsqlc.Membership{}, err
	}
	defer tx.Rollback(ctx)
	result, err := s.createRequestTx(ctx, tx, r, nil)
	if err != nil {
		return dbsqlc.Membership{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return dbsqlc.Membership{}, err
	}
	return result, nil
}

// CreateRequestWithPresentedConsentsTx reuses all membership rules. The caller
// supplies the authenticated public presentation, including an empty snapshot.
func (s *Service) CreateRequestWithPresentedConsentsTx(ctx context.Context, tx pgx.Tx, r Request, presented []PresentedConsent) (dbsqlc.Membership, error) {
	if presented == nil {
		presented = []PresentedConsent{}
	}
	return s.createRequestTx(ctx, tx, r, presented)
}

func (s *Service) createRequestTx(ctx context.Context, tx pgx.Tx, r Request, presented []PresentedConsent) (dbsqlc.Membership, error) {
	var zero dbsqlc.Membership
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidRequest, reason) }
	if len(r.ActivityIDs) == 0 {
		return zero, invalid("missing_activity")
	}
	var err error
	var birth pgtype.Date
	err = tx.QueryRow(ctx, "SELECT birth_date FROM persons WHERE id=$1 FOR NO KEY UPDATE", r.PersonID).Scan(&birth)
	if err != nil {
		return zero, err
	}
	if !birth.Valid || birth.InfinityModifier != pgtype.Finite {
		return zero, invalid("missing_birth_date")
	}
	for _, check := range []struct {
		sql string
		id  int32
	}{
		{"SELECT is_active FROM seasons WHERE id=$1 FOR SHARE", r.SeasonID},
		{"SELECT is_active FROM membership_types WHERE id=$1 FOR SHARE", r.MembershipTypeID},
	} {
		var active bool
		if err = tx.QueryRow(ctx, check.sql, check.id).Scan(&active); err != nil {
			return zero, err
		}
		if !active {
			return zero, invalid("inactive season or membership type")
		}
	}
	seen := map[int32]bool{}
	for _, id := range r.ActivityIDs {
		if seen[id] {
			return zero, invalid("duplicate activity")
		}
		seen[id] = true
		var active bool
		if err = tx.QueryRow(ctx, "SELECT is_active FROM activities WHERE id=$1 FOR SHARE", id).Scan(&active); err != nil {
			return zero, err
		}
		if !active {
			return zero, invalid("inactive activity")
		}
	}
	// nil means the existing workflow snapshots currently active definitions.
	// A non-nil slice is an already authenticated, immutable public presentation.
	if presented == nil {
		if _, err = tx.Exec(ctx, "LOCK TABLE consent_definitions IN SHARE MODE"); err != nil {
			return zero, err
		}
		defs, e := dbsqlc.New(tx).ListActiveConsentDefinitions(ctx)
		if e != nil {
			return zero, e
		}
		presented = make([]PresentedConsent, 0, len(defs))
		for _, d := range defs {
			presented = append(presented, PresentedConsent{ConsentDefinitionID: d.ID})
		}
	}
	answers := map[int32]Decision{}
	for _, d := range r.Consents {
		if _, ok := answers[d.ConsentDefinitionID]; ok {
			return zero, invalid("duplicate consent")
		}
		if d.Decision != "granted" && d.Decision != "refused" {
			return zero, consents.ErrInvalidDecision
		}
		answers[d.ConsentDefinitionID] = d
	}
	if len(answers) != len(presented) {
		return zero, invalid("unanswered or unexpected consent")
	}
	var id int32
	err = tx.QueryRow(ctx, "INSERT INTO memberships(person_id,season_id,membership_type_id,status,requested_at) VALUES ($1,$2,$3,'pending',clock_timestamp()) RETURNING id", r.PersonID, r.SeasonID, r.MembershipTypeID).Scan(&id)
	if err != nil {
		return zero, err
	}
	for _, activity := range r.ActivityIDs {
		if _, err = tx.Exec(ctx, "INSERT INTO membership_activities VALUES ($1,$2)", id, activity); err != nil {
			return zero, err
		}
	}
	for _, def := range presented {
		d, ok := answers[def.ConsentDefinitionID]
		if !ok {
			return zero, invalid("unanswered_consent")
		}
		if d.GivenByPersonID != r.PersonID {
			_, err = dbsqlc.New(tx).LockConsentGuardian(ctx, dbsqlc.LockConsentGuardianParams{ChildPersonID: r.PersonID, GuardianPersonID: d.GivenByPersonID})
			if errors.Is(err, pgx.ErrNoRows) {
				return zero, consents.ErrUnauthorizedGiver
			}
			if err != nil {
				return zero, err
			}
		}
		if _, err = tx.Exec(ctx, "INSERT INTO membership_consent_requirements SELECT id,$2,COALESCE($3,requested_at) FROM memberships WHERE id=$1", id, def.ConsentDefinitionID, def.PresentedAt); err != nil {
			return zero, err
		}
		_, err = dbsqlc.New(tx).CreateMembershipConsent(ctx, dbsqlc.CreateMembershipConsentParams{MembershipID: id, ConsentDefinitionID: def.ConsentDefinitionID, Decision: d.Decision, GivenByPersonID: d.GivenByPersonID})
		if err != nil {
			return zero, err
		}
	}
	result, err := dbsqlc.New(tx).GetMembership(ctx, id)
	if err != nil {
		return zero, err
	}
	return result, nil
}

// IsMinor compares civil dates, with the eighteenth anniversary on March 1 for
// February 29 births in non-leap years. No age or minority flag is persisted.
func IsMinor(birth, at time.Time) bool {
	return civildate.IsMinor(birth, at)
}

func completeness(ctx context.Context, tx pgx.Tx, id int32, at time.Time) (Completeness, error) {
	c := Completeness{BlockingIssues: []string{}, Warnings: []string{}, MissingConsentDefinitionIDs: []int32{}}
	facts, err := dbsqlc.New(tx).ListMembershipCompletenessFacts(ctx, []int32{id})
	if err != nil {
		return c, err
	}
	if len(facts) != 1 {
		return c, pgx.ErrNoRows
	}
	return evaluateCompleteness(facts[0], at), nil
}

// evaluateCompleteness is shared by detail, approval and the batched list.
func evaluateCompleteness(f dbsqlc.ListMembershipCompletenessFactsRow, at time.Time) Completeness {
	c := Completeness{BlockingIssues: []string{}, Warnings: []string{}, MissingConsentDefinitionIDs: f.MissingConsentIds}
	birth, activity, guardian, emergency := f.BirthDate, f.HasActivity, f.HasGuardian, f.HasEmergency

	if !birth.Valid || birth.InfinityModifier != pgtype.Finite {
		c.BlockingIssues = append(c.BlockingIssues, "missing_birth_date")
	} else {
		minor := IsMinor(birth.Time, at)
		c.IsMinor = &minor
		if minor {
			if !guardian {
				c.BlockingIssues = append(c.BlockingIssues, "minor_missing_guardian")
			}
			if !emergency {
				c.BlockingIssues = append(c.BlockingIssues, "minor_missing_emergency")
			}
		} else if !emergency {
			c.Warnings = append(c.Warnings, "adult_missing_emergency")
		}
	}
	if !activity {
		c.BlockingIssues = append(c.BlockingIssues, "missing_activity")
	}

	if len(c.MissingConsentDefinitionIDs) > 0 {
		c.BlockingIssues = append(c.BlockingIssues, "unanswered_consent")
	}
	return c
}

func (s *Service) GetDetails(ctx context.Context, id int32) (Details, error) {
	var d Details
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return d, err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	d.Membership, err = q.GetMembershipDetails(ctx, id)
	if err != nil {
		return d, err
	}
	d.Account = AccountState{
		Exists:          d.Membership.UserID.Valid,
		IsActive:        d.Membership.UserIsActive.Bool,
		IsActivated:     d.Membership.ActivatedAt.Valid,
		NeedsActivation: d.Membership.UserID.Valid && !d.Membership.ActivatedAt.Valid,
	}
	d.Activities, err = q.ListMembershipActivities(ctx, id)
	if err != nil {
		return d, err
	}
	d.ConsentRequirements, err = q.ListMembershipConsentRequirements(ctx, id)
	if err != nil {
		return d, err
	}
	d.Guardians, err = q.ListPersonGuardians(ctx, d.Membership.Person.ID)
	if err != nil {
		return d, err
	}
	d.EmergencyContacts, err = q.ListPersonEmergencyContacts(ctx, d.Membership.Person.ID)
	if err != nil {
		return d, err
	}
	d.Completeness, err = completeness(ctx, tx, id, time.Now().In(s.location))
	if err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}

func (s *Service) ApproveMembership(ctx context.Context, id, approver int32, adminNote *string) (Approval, error) {
	var result Approval
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var person int32
	// Person lock serializes account reuse across memberships in different seasons.
	if err = tx.QueryRow(ctx, "SELECT person_id FROM memberships WHERE id=$1", id).Scan(&person); err != nil {
		return result, err
	}
	var first, last string
	if err = tx.QueryRow(ctx, "SELECT first_name,last_name FROM persons WHERE id=$1 FOR UPDATE", person).Scan(&first, &last); err != nil {
		return result, err
	}
	var status string
	var lockedPerson int32
	if err = tx.QueryRow(ctx, "SELECT status,person_id FROM memberships WHERE id=$1 FOR UPDATE", id).Scan(&status, &lockedPerson); err != nil {
		return result, err
	}
	if person != lockedPerson {
		return result, ErrInvalidRequest
	}
	if status != "pending" {
		return result, ErrNotPending
	}
	// Protect the observed activities and contact relationships against concurrent deletion.
	for _, lock := range []struct {
		query string
		id    int32
	}{
		{"SELECT activity_id FROM membership_activities WHERE membership_id=$1 FOR SHARE", id},
		{"SELECT id FROM person_guardians WHERE child_person_id=$1 FOR SHARE", person},
		{"SELECT id FROM person_emergency_contacts WHERE person_id=$1 FOR SHARE", person},
	} {
		rows, e := tx.Query(ctx, lock.query, lock.id)
		if e != nil {
			return result, e
		}
		for rows.Next() {
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
	}
	var exists int32
	if err = tx.QueryRow(ctx, "SELECT id FROM users WHERE id=$1 FOR KEY SHARE", approver).Scan(&exists); err != nil {
		return result, fmt.Errorf("approver: %w", err)
	}
	now := time.Now().In(s.location)
	result.Completeness, err = completeness(ctx, tx, id, now)
	if err != nil {
		return result, err
	}
	if len(result.Completeness.BlockingIssues) > 0 {
		return result, &IncompleteError{result.Completeness}
	}
	q := dbsqlc.New(tx)
	user, err := provisioning.EnsureUserForPersonTx(ctx, tx, person)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, "UPDATE users SET is_active=true WHERE person_id=$1", person)
	if err != nil {
		return result, err
	}
	user, err = q.GetUserByPerson(ctx, person)
	if err != nil {
		return result, err
	}
	result.User = user
	result.ActivationDelivery, err = s.activation.PrepareTx(ctx, tx, user.ID)
	if err != nil {
		return Approval{}, err
	}
	_, err = tx.Exec(ctx, "UPDATE memberships SET status='active',approved_at=$2,approved_by_user_id=$3,admin_note=$4,joined_at=COALESCE(joined_at,$5::date),updated_at=$2 WHERE id=$1", id, now, approver, adminNote, now.Format("2006-01-02"))
	if err != nil {
		return Approval{}, err
	}
	result.Membership, err = q.GetMembership(ctx, id)
	if err != nil {
		return Approval{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Approval{}, err
	}
	return result, nil
}
