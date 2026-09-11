// Package memberships owns submission, computed completeness and approval.
// Callers must use this service for workflow writes.
package memberships

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/grapinou/club-core/internal/activation"
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

func (s *Service) CreateRequest(ctx context.Context, r Request) (dbsqlc.Membership, error) {
	var zero dbsqlc.Membership
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidRequest, reason) }
	if len(r.ActivityIDs) == 0 {
		return zero, invalid("missing_activity")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	var birth pgtype.Date
	err = tx.QueryRow(ctx, "SELECT birth_date FROM persons WHERE id=$1 FOR UPDATE", r.PersonID).Scan(&birth)
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
	// One statement defines the submission snapshot. SHARE prevents publication/
	// deactivation during submission, including insertion of new definitions.
	if _, err = tx.Exec(ctx, "LOCK TABLE consent_definitions IN SHARE MODE"); err != nil {
		return zero, err
	}
	defs, err := dbsqlc.New(tx).ListActiveConsentDefinitions(ctx)
	if err != nil {
		return zero, err
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
	if len(answers) != len(defs) {
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
	for _, def := range defs {
		d, ok := answers[def.ID]
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
		if _, err = tx.Exec(ctx, "INSERT INTO membership_consent_requirements SELECT id,$2,requested_at FROM memberships WHERE id=$1", id, def.ID); err != nil {
			return zero, err
		}
		_, err = dbsqlc.New(tx).CreateMembershipConsent(ctx, dbsqlc.CreateMembershipConsentParams{MembershipID: id, ConsentDefinitionID: def.ID, Decision: d.Decision, GivenByPersonID: d.GivenByPersonID})
		if err != nil {
			return zero, err
		}
	}
	result, err := dbsqlc.New(tx).GetMembership(ctx, id)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}

// IsMinor compares civil dates, with the eighteenth anniversary on March 1 for
// February 29 births in non-leap years. No age or minority flag is persisted.
func IsMinor(birth, at time.Time) bool {
	anniversary := time.Date(birth.Year()+18, birth.Month(), birth.Day(), 0, 0, 0, 0, at.Location())
	today := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
	return today.Before(anniversary)
}

func completeness(ctx context.Context, tx pgx.Tx, id int32, at time.Time) (Completeness, error) {
	c := Completeness{BlockingIssues: []string{}, Warnings: []string{}, MissingConsentDefinitionIDs: []int32{}}
	var birth pgtype.Date
	var activity, guardian, emergency bool
	err := tx.QueryRow(ctx, `SELECT p.birth_date,
 EXISTS(SELECT 1 FROM membership_activities WHERE membership_id=m.id),
 EXISTS(SELECT 1 FROM person_guardians WHERE child_person_id=p.id),
 EXISTS(SELECT 1 FROM person_emergency_contacts WHERE person_id=p.id)
 FROM memberships m JOIN persons p ON p.id=m.person_id WHERE m.id=$1`, id).Scan(&birth, &activity, &guardian, &emergency)
	if err != nil {
		return c, err
	}
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
	rows, err := tx.Query(ctx, `SELECT r.consent_definition_id FROM membership_consent_requirements r
 WHERE r.membership_id=$1 AND NOT EXISTS (
 SELECT 1 FROM membership_consents c WHERE c.membership_id=r.membership_id
 AND c.consent_definition_id=r.consent_definition_id
 AND c.id = (SELECT initial.id FROM membership_consents initial WHERE initial.membership_id=r.membership_id AND initial.consent_definition_id=r.consent_definition_id ORDER BY initial.recorded_at,initial.id LIMIT 1)
 AND c.decision IN ('granted','refused'))
 ORDER BY r.consent_definition_id`, id)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var def int32
		if err = rows.Scan(&def); err != nil {
			return c, err
		}
		c.MissingConsentDefinitionIDs = append(c.MissingConsentDefinitionIDs, def)
	}
	if len(c.MissingConsentDefinitionIDs) > 0 {
		c.BlockingIssues = append(c.BlockingIssues, "unanswered_consent")
	}
	return c, rows.Err()
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
	user, err := q.GetUserByPerson(ctx, person)
	if errors.Is(err, pgx.ErrNoRows) {
		base := UsernameBase(first, last)
		for suffix := 1; ; suffix++ {
			username := base
			if suffix > 1 {
				username += strconv.Itoa(suffix)
			}
			var uid int32
			err = tx.QueryRow(ctx, "INSERT INTO users(person_id,username,is_active) VALUES ($1,$2,true) ON CONFLICT (username) DO NOTHING RETURNING id", person, username).Scan(&uid)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return result, err
			}
			break
		}
	} else if err != nil {
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
