// Package administration composes office capabilities without personal ownership
// or GuardianAccess shortcuts. Mutations audit only actor/action/resource IDs.
package administration

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrConflict = errors.New("dossier changed; reload before retrying")
var ErrInvalid = errors.New("invalid administrative input")

type Service struct {
	db          *pgxpool.Pool
	q           *dbsqlc.Queries
	permissions *authorization.Service
	trials      *trials.Service
	memberships *memberships.Service
	loc         *time.Location
}

func New(db *pgxpool.Pool, p *authorization.Service, m *memberships.Service, loc *time.Location) *Service {
	return &Service{db: db, q: dbsqlc.New(db), permissions: p, trials: trials.New(db), memberships: m, loc: loc}
}
func (s *Service) Today() pgtype.Date {
	now := time.Now().In(s.loc)
	return pgtype.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
func (s *Service) require(ctx context.Context, p authorization.Permission) (int32, error) {
	id, ok := auth.UserID(ctx)
	if !ok {
		return 0, authorization.ErrForbidden
	}
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return 0, authorization.ErrForbidden
	}
	if !u.IsActive || !u.ActivatedAt.Valid || !u.PasswordHash.Valid {
		return 0, authorization.ErrForbidden
	}
	allowed, err := s.permissions.HasPermission(ctx, id, p)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, authorization.ErrForbidden
	}
	return id, nil
}
func (s *Service) mutate(ctx context.Context, p authorization.Permission, fn func(pgx.Tx, *dbsqlc.Queries) (string, string, int32, error)) error {
	actor, err := s.require(ctx, p)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := dbsqlc.New(tx)
	action, kind, id, err := fn(tx, q)
	if err != nil {
		return err
	}
	if err = q.CreateAdministrativeEvent(ctx, dbsqlc.CreateAdministrativeEventParams{ActorUserID: actor, Action: action, ResourceType: kind, ResourceID: id}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func Note(value string) (pgtype.Text, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || utf8.RuneCountInString(value) > 10000 {
		return pgtype.Text{}, ErrInvalid
	}
	return pgtype.Text{String: value, Valid: value != ""}, nil
}

type Home struct {
	Counts      dbsqlc.AdministrativeCountsRow
	Upcoming    []dbsqlc.AdministrativeTrialsRow
	PastPending []dbsqlc.AdministrativeTrialsRow
}

func (s *Service) Dashboard(ctx context.Context) (Home, error) {
	var d Home
	if _, err := s.require(ctx, authorization.PersonsRead); err != nil {
		return d, err
	}
	if _, err := s.require(ctx, authorization.MembershipsRead); err != nil {
		return d, err
	}
	today := s.Today()
	var err error
	d.Counts, err = s.q.AdministrativeCounts(ctx, today)
	if err != nil {
		return d, err
	}
	d.Upcoming, err = s.q.AdministrativeTrials(ctx, dbsqlc.AdministrativeTrialsParams{FromDate: today, Today: today})
	if err != nil {
		return d, err
	}
	if len(d.Upcoming) > 8 {
		d.Upcoming = d.Upcoming[:8]
	}
	d.PastPending, err = s.q.AdministrativeTrials(ctx, dbsqlc.AdministrativeTrialsParams{BeforeDate: today, Today: today})
	if len(d.PastPending) > 8 {
		d.PastPending = d.PastPending[:8]
	}
	return d, err
}
func (s *Service) People(ctx context.Context, search string, page int32) ([]dbsqlc.SearchAdministrativePersonsRow, error) {
	if _, err := s.require(ctx, authorization.PersonsRead); err != nil {
		return nil, err
	}
	if page < 0 || page > 10000 || len(search) > 254 {
		return nil, ErrInvalid
	}
	return s.q.SearchAdministrativePersons(ctx, dbsqlc.SearchAdministrativePersonsParams{Search: strings.TrimSpace(search), Phone: identityresolution.NormalizePhone(search), PageOffset: page * 50})
}

type Person struct {
	TrialQuotas []trials.QuotaUsage
	IsMinor     bool
	Account     []dbsqlc.AdministrativePersonAccountRow
	Info        dbsqlc.AdministrativePersonRow
	Relations   []dbsqlc.AdministrativeRelationsRow
	Trials      []dbsqlc.AdministrativeTrialsRow
	Memberships []dbsqlc.AdministrativeMembershipsRow
}

func (s *Service) Person(ctx context.Context, id int32) (Person, error) {
	var p Person
	if _, err := s.require(ctx, authorization.PersonsRead); err != nil {
		return p, err
	}
	var err error
	p.Info, err = s.q.AdministrativePerson(ctx, id)
	if err != nil {
		return p, err
	}
	p.IsMinor = s.memberships.IsEligibleMinor(p.Info.BirthDate)
	p.Account, err = s.q.AdministrativePersonAccount(ctx, id)
	if err != nil {
		return p, err
	}
	p.Relations, err = s.q.AdministrativeRelations(ctx, id)
	if err != nil {
		return p, err
	}
	p.Trials, err = s.q.AdministrativeTrials(ctx, dbsqlc.AdministrativeTrialsParams{PersonID: id, Today: s.Today()})
	if err != nil {
		return p, err
	}
	p.TrialQuotas, err = s.trials.Quotas(ctx, id)
	if err != nil {
		return p, err
	}
	if _, err = s.require(ctx, authorization.MembershipsRead); err != nil {
		return p, err
	}
	p.Memberships, err = s.q.AdministrativeMemberships(ctx, dbsqlc.AdministrativeMembershipsParams{PersonID: id, Today: s.Today()})
	return p, err
}

func (s *Service) TrialQuota(ctx context.Context, id int32) ([]trials.QuotaUsage, error) {
	t, err := s.Trial(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.trials.QuotaForTrial(ctx, dbsqlc.TrialRegistration{ID: t.ID, PersonID: t.PersonID})
}

// AddGuardianEmergency adds a known legal guardian without replacing other
// contacts. Person locking serializes priority allocation with approval.
func (s *Service) AddGuardianEmergency(ctx context.Context, child, guardian int32) error {
	return s.mutate(ctx, authorization.PersonsWrite, func(tx pgx.Tx, q *dbsqlc.Queries) (string, string, int32, error) {
		first, second := child, guardian
		if first > second {
			first, second = second, first
		}
		for _, id := range []int32{first, second} {
			if _, err := q.LockAdministrativePerson(ctx, id); err != nil {
				return "", "", 0, err
			}
		}
		if _, err := q.LockConsentGuardian(ctx, dbsqlc.LockConsentGuardianParams{ChildPersonID: child, GuardianPersonID: guardian}); err != nil {
			return "", "", 0, err
		}
		_, err := tx.Exec(ctx, `INSERT INTO person_emergency_contacts(person_id,contact_person_id,priority)
		 SELECT $1,$2,coalesce(max(priority),0)+1 FROM person_emergency_contacts WHERE person_id=$1
		 ON CONFLICT (person_id,contact_person_id) DO NOTHING`, child, guardian)
		return "emergency_contact_added", "person", child, err
	})
}
func (s *Service) Trials(ctx context.Context, on, from pgtype.Date) ([]dbsqlc.AdministrativeTrialsRow, error) {
	if _, err := s.require(ctx, authorization.PersonsRead); err != nil {
		return nil, err
	}
	return s.q.AdministrativeTrials(ctx, dbsqlc.AdministrativeTrialsParams{OnDate: on, FromDate: from, Today: s.Today()})
}
func (s *Service) PastPendingTrials(ctx context.Context) ([]dbsqlc.AdministrativeTrialsRow, error) {
	if _, err := s.require(ctx, authorization.PersonsRead); err != nil {
		return nil, err
	}
	today := s.Today()
	return s.q.AdministrativeTrials(ctx, dbsqlc.AdministrativeTrialsParams{BeforeDate: today, Today: today})
}
func (s *Service) Trial(ctx context.Context, id int32) (dbsqlc.AdministrativeTrialsRow, error) {
	if _, err := s.require(ctx, authorization.PersonsRead); err != nil {
		return dbsqlc.AdministrativeTrialsRow{}, err
	}
	rows, err := s.q.AdministrativeTrials(ctx, dbsqlc.AdministrativeTrialsParams{TrialID: id, Today: s.Today()})
	if err != nil {
		return dbsqlc.AdministrativeTrialsRow{}, err
	}
	if len(rows) != 1 {
		return dbsqlc.AdministrativeTrialsRow{}, pgx.ErrNoRows
	}
	return rows[0], nil
}
func (s *Service) Relations(ctx context.Context, personID int32) ([]dbsqlc.AdministrativeRelationsRow, error) {
	if _, err := s.require(ctx, authorization.PersonsRead); err != nil {
		return nil, err
	}
	return s.q.AdministrativeRelations(ctx, personID)
}

type Choices struct {
	Activities []dbsqlc.AdministrativeActivitiesRow
	Groups     []dbsqlc.ListActiveGroupsRow
	Slots      []dbsqlc.AdministrativeSlotsRow
	Seasons    []dbsqlc.AdministrativeSeasonsRow
	Types      []dbsqlc.AdministrativeMembershipTypesRow
	Consents   []dbsqlc.ConsentDefinition
}

func (s *Service) Choices(ctx context.Context) (Choices, error) {
	var c Choices
	if _, err := s.require(ctx, authorization.PersonsRead); err != nil {
		return c, err
	}
	var err error
	c.Activities, err = s.q.AdministrativeActivities(ctx)
	if err != nil {
		return c, err
	}
	c.Groups, err = s.q.ListActiveGroups(ctx)
	if err != nil {
		return c, err
	}
	c.Slots, err = s.q.AdministrativeSlots(ctx)
	if err != nil {
		return c, err
	}
	c.Seasons, err = s.q.AdministrativeSeasons(ctx)
	if err != nil {
		return c, err
	}
	c.Types, err = s.q.AdministrativeMembershipTypes(ctx)
	if err != nil {
		return c, err
	}
	c.Consents, err = s.q.ListActiveConsentDefinitions(ctx)
	return c, err
}
func (s *Service) UpdatePersonNotes(ctx context.Context, id int32, value string) error {
	notes, err := Note(value)
	if err != nil {
		return err
	}
	return s.mutate(ctx, authorization.PersonsWrite, func(tx pgx.Tx, q *dbsqlc.Queries) (string, string, int32, error) {
		if _, err := q.LockAdministrativePerson(ctx, id); err != nil {
			return "", "", 0, err
		}
		return "person_notes_updated", "person", id, q.UpdateAdministrativePersonNotes(ctx, dbsqlc.UpdateAdministrativePersonNotesParams{ID: id, Notes: notes})
	})
}
func (s *Service) Schedule(ctx context.Context, p dbsqlc.CreateTrialParams) (int32, error) {
	var id int32
	notes, err := Note(p.Notes.String)
	if err != nil {
		return 0, err
	}
	p.Notes = notes
	err = s.mutate(ctx, authorization.PersonsWrite, func(tx pgx.Tx, q *dbsqlc.Queries) (string, string, int32, error) {
		t, err := s.trials.ScheduleTx(ctx, tx, p)
		id = t.ID
		return "trial_scheduled", "trial", id, err
	})
	return id, err
}
func (s *Service) UpdateTrial(ctx context.Context, id, revision int32, action string, schedule dbsqlc.RescheduleTrialParams, value string) error {
	return s.mutate(ctx, authorization.PersonsWrite, func(tx pgx.Tx, q *dbsqlc.Queries) (string, string, int32, error) {
		t, err := s.trials.LockForUpdate(ctx, tx, id)
		if err != nil {
			return "", "", 0, err
		}
		if t.Revision != revision {
			return "", "", 0, ErrConflict
		}
		switch action {
		case "reschedule":
			schedule.ID = id
			_, err = s.trials.RescheduleTx(ctx, tx, schedule)
		case "status":
			_, err = s.trials.UpdateStatusTx(ctx, tx, dbsqlc.UpdateTrialStatusParams{ID: id, Status: value})
		case "notes":
			var note pgtype.Text
			note, err = Note(value)
			if err == nil {
				_, err = s.trials.UpdateNotesTx(ctx, tx, dbsqlc.UpdateTrialNotesParams{ID: id, Notes: note})
			}
		default:
			return "", "", 0, ErrInvalid
		}
		event := map[string]string{"reschedule": "trial_rescheduled", "status": "trial_status_updated", "notes": "trial_notes_updated"}[action]
		return event, "trial", id, err
	})
}
func (s *Service) RequestMembership(ctx context.Context, r memberships.Request, sourceTrial int32) (int32, error) {
	var id int32
	err := s.mutate(ctx, authorization.MembershipsApprove, func(tx pgx.Tx, q *dbsqlc.Queries) (string, string, int32, error) {
		if _, err := q.LockAdministrativePerson(ctx, r.PersonID); err != nil {
			return "", "", 0, err
		}
		r.SourceTrialID = sourceTrial
		m, err := s.memberships.CreateRequestTx(ctx, tx, r)
		id = m.ID
		return "membership_requested", "membership", id, err
	})
	return id, err
}

type Membership struct {
	Info    dbsqlc.Membership
	History []dbsqlc.ListMembershipGroupHistoryRow
}

func (s *Service) Membership(ctx context.Context, id int32) (Membership, error) {
	var m Membership
	if _, err := s.require(ctx, authorization.MembershipsRead); err != nil {
		return m, err
	}
	var err error
	m.Info, err = s.q.GetMembership(ctx, id)
	if err != nil {
		return m, err
	}
	m.History, err = s.q.ListMembershipGroupHistory(ctx, id)
	return m, err
}
func (s *Service) UpdateMembershipNotes(ctx context.Context, id int32, value string) error {
	n, err := Note(value)
	if err != nil {
		return err
	}
	return s.mutate(ctx, authorization.MembershipsApprove, func(tx pgx.Tx, q *dbsqlc.Queries) (string, string, int32, error) {
		if _, err := q.LockAdministrativeMembership(ctx, id); err != nil {
			return "", "", 0, err
		}
		return "membership_notes_updated", "membership", id, q.UpdateAdministrativeMembershipNotes(ctx, dbsqlc.UpdateAdministrativeMembershipNotesParams{ID: id, AdminNote: n})
	})
}
func (s *Service) AssignGroup(ctx context.Context, p dbsqlc.AssignMembershipGroupParams) error {
	return s.mutate(ctx, authorization.MembershipsApprove, func(tx pgx.Tx, q *dbsqlc.Queries) (string, string, int32, error) {
		return "membership_group_assigned", "membership", p.MembershipID, s.memberships.AssignGroupTx(ctx, tx, p)
	})
}
func (s *Service) CloseGroup(ctx context.Context, membership, assignment int32, left pgtype.Date) error {
	return s.mutate(ctx, authorization.MembershipsApprove, func(tx pgx.Tx, q *dbsqlc.Queries) (string, string, int32, error) {
		return "membership_group_closed", "membership", membership, s.memberships.CloseGroupTx(ctx, tx, membership, assignment, left)
	})
}
