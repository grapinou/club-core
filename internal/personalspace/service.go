// Package personalspace provides read-only, minimized personal and family data.
// Administrative roles never widen this service's resource authorization.
package personalspace

import (
	"context"
	"errors"
	"fmt"
	"time"

	"database/sql"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/civildate"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"github.com/grapinou/club-core/internal/memberships"
)

var ErrNotFound = errors.New("personal resource unavailable")

type GuardianAccess interface {
	CanManageChild(context.Context, int32) (bool, error)
	ListManagedChildren(context.Context) ([]guardianaccess.RelatedPerson, error)
}

type Service struct {
	q         *dbsqlc.Queries
	guardians GuardianAccess
	location  *time.Location
}

func New(q *dbsqlc.Queries, guardians GuardianAccess, location *time.Location) *Service {
	if location == nil {
		panic("personal space requires business location")
	}
	return &Service{q: q, guardians: guardians, location: location}
}

// DTOs intentionally exclude administrative notes, identity candidates, credentials,
// unrelated contact details and consent giver identity. Authorized emergency
// contacts are projected minimally. IDs serve links only.
type Account struct {
	CanManageEmergency                                              bool
	FirstName, LastName, Username, Email, Phone, Address, BirthDate string
	Functions                                                       []string
}
type Summary struct {
	ID, SeasonID                           int32
	SeasonName, MembershipTypeName, Status string
	Activities                             []string
	Groups                                 []string
	RequestedAt                            string
}
type FamilyAction struct{ Season, Label, URL string }
type ChildSummary struct {
	EmergencyContacts []dbsqlc.ListPersonalEmergencyContactsRow
	Actions           []FamilyAction
	ID                int32
	Name              string
	Memberships       []Summary
}
type FamilyRequest struct{ Name, ReceivedAt, URL string }
type Dashboard struct {
	EmergencyContacts  []dbsqlc.ListPersonalEmergencyContactsRow
	CanManageEmergency bool
	PendingRequests    []FamilyRequest
	Name               string
	Memberships        []Summary
	Children           []ChildSummary
}
type Child struct {
	EmergencyContacts               []dbsqlc.ListPersonEmergencyContactsRow
	Actions                         []FamilyAction
	ID                              int32
	Name, BirthDate                 string
	HasEmergency, ViewerIsEmergency bool
	Memberships                     []Summary
}
type Consent struct {
	ID                                       int32
	Editable                                 bool
	Title, Description, Decision, RecordedAt string
	Version                                  int32
	GivenByViewer                            bool
}
type Slot struct{ Day, Start, End, Location string }
type Group struct {
	Name, Activity string
	Slots          []Slot
}
type Membership struct {
	CanDecide bool
	Summary
	RequestedAt, JoinedAt      string
	Complete, MissingEmergency bool
	Consents                   []Consent
	Groups                     []Group
}

func unavailable(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func (s *Service) identity(ctx context.Context) (dbsqlc.GetPersonalAccountRow, error) {
	id, ok := auth.UserID(ctx)
	if !ok {
		return dbsqlc.GetPersonalAccountRow{}, ErrNotFound
	}
	a, err := s.q.GetPersonalAccount(ctx, id)
	return a, unavailable(err)
}
func (s *Service) GetMyAccount(ctx context.Context) (Account, error) {
	a, err := s.identity(ctx)
	if err != nil {
		return Account{}, err
	}
	id, _ := auth.UserID(ctx)
	roles, err := s.q.ListUserRoles(ctx, id)
	if err != nil {
		return Account{}, err
	}
	account := Account{CanManageEmergency: s.canManageOwnEmergency(a.BirthDate), FirstName: a.FirstName, LastName: a.LastName, Username: a.Username, Email: a.Email.String, Phone: a.PhoneNumber.String, Address: a.Address.String}
	if a.BirthDate.Valid {
		account.BirthDate = a.BirthDate.Time.Format("02/01/2006")
	}
	labels := map[string]string{"president": "Présidence", "secretary": "Secrétariat", "treasurer": "Trésorerie", "coach": "Encadrement sportif"}
	for _, role := range roles {
		if label, ok := labels[role.Name]; ok {
			account.Functions = append(account.Functions, label)
		}
	}
	return account, nil
}
func (s *Service) summary(r dbsqlc.ListPersonalMembershipSummariesRow) Summary {
	return Summary{ID: r.ID, SeasonID: r.SeasonID, SeasonName: r.SeasonName, MembershipTypeName: r.MembershipTypeName, Status: r.Status, Activities: r.Activities, Groups: r.GroupNames, RequestedAt: r.RequestedAt.Time.In(s.location).Format("02/01/2006")}
}
func (s *Service) GetDashboard(ctx context.Context) (Dashboard, error) {
	a, err := s.identity(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	children, err := s.guardians.ListManagedChildren(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	ids := []int32{a.PersonID}
	for _, c := range children {
		ids = append(ids, c.PersonID)
	}
	// Exactly one membership read for all authorized Persons, including no children.
	rows, err := s.q.ListPersonalMembershipSummaries(ctx, dbsqlc.ListPersonalMembershipSummariesParams{Ids: ids, Today: s.today()})
	if err != nil {
		return Dashboard{}, err
	}
	byPerson := map[int32][]Summary{}
	for _, r := range rows {
		byPerson[r.PersonID] = append(byPerson[r.PersonID], s.summary(r))
	}
	seasons, err := s.q.ListRegistrationSeasons(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	d := Dashboard{CanManageEmergency: s.canManageOwnEmergency(a.BirthDate), Name: a.FirstName + " " + a.LastName, Memberships: byPerson[a.PersonID]}
	actor, _ := auth.UserID(ctx)
	managed := make([]int32, 0, len(children))
	for _, c := range children {
		managed = append(managed, c.PersonID)
	}
	pending, err := s.q.ListPendingFamilyRequests(ctx, dbsqlc.ListPendingFamilyRequestsParams{ViewerUserID: sql.NullInt32{Int32: actor, Valid: true}, ViewerPersonID: sql.NullInt32{Int32: a.PersonID, Valid: true}, ManagedChildren: nullableIDs(managed)})
	if err != nil {
		return Dashboard{}, err
	}
	for _, r := range pending {
		d.PendingRequests = append(d.PendingRequests, FamilyRequest{Name: r.FirstName + " " + r.LastName, ReceivedAt: r.CreatedAt.Time.In(s.location).Format("02/01/2006")})
	}

	emergency, err := s.q.ListPersonalEmergencyContacts(ctx, ids)
	if err != nil {
		return Dashboard{}, err
	}
	byEmergency := map[int32][]dbsqlc.ListPersonalEmergencyContactsRow{}
	for _, c := range emergency {
		byEmergency[c.PersonID] = append(byEmergency[c.PersonID], c)
	}
	if d.CanManageEmergency {
		d.EmergencyContacts = byEmergency[a.PersonID]
	}
	for _, m := range d.Memberships {
		if m.Status == "pending" {
			d.PendingRequests = append(d.PendingRequests, FamilyRequest{Name: d.Name, ReceivedAt: m.RequestedAt, URL: fmt.Sprintf("/me/memberships/%d", m.ID)})
		}
	}
	for _, c := range children {
		for _, m := range byPerson[c.PersonID] {
			if m.Status == "pending" {
				d.PendingRequests = append(d.PendingRequests, FamilyRequest{Name: c.FirstName + " " + c.LastName, ReceivedAt: m.RequestedAt, URL: fmt.Sprintf("/me/children/%d/memberships/%d", c.PersonID, m.ID)})
			}
		}
		d.Children = append(d.Children, ChildSummary{EmergencyContacts: byEmergency[c.PersonID], ID: c.PersonID, Name: c.FirstName + " " + c.LastName, Memberships: byPerson[c.PersonID], Actions: familyActions(c.PersonID, byPerson[c.PersonID], seasons)})
	}
	return d, nil
}
func (s *Service) authorizeChild(ctx context.Context, child int32) (dbsqlc.GetPersonalAccountRow, error) {
	a, err := s.identity(ctx)
	if err != nil {
		return a, err
	}
	allowed, err := s.guardians.CanManageChild(ctx, child)
	if err != nil {
		return a, err
	}
	if !allowed {
		return a, ErrNotFound
	}
	return a, nil
}
func (s *Service) GetManagedChild(ctx context.Context, child int32) (Child, error) {
	a, err := s.authorizeChild(ctx, child)
	if err != nil {
		return Child{}, err
	}
	p, err := s.q.GetPersonalChild(ctx, dbsqlc.GetPersonalChildParams{ChildPersonID: child, ViewerPersonID: a.PersonID})
	if err != nil {
		return Child{}, unavailable(err)
	}
	rows, err := s.q.ListPersonalMembershipSummaries(ctx, dbsqlc.ListPersonalMembershipSummariesParams{Ids: []int32{child}, Today: s.today()})
	if err != nil {
		return Child{}, err
	}
	c := Child{ID: child, Name: p.FirstName + " " + p.LastName, BirthDate: p.BirthDate.Time.Format("02/01/2006"), HasEmergency: p.HasEmergency, ViewerIsEmergency: p.ViewerIsEmergency}
	for _, r := range rows {
		c.Memberships = append(c.Memberships, s.summary(r))
	}
	seasons, err := s.q.ListRegistrationSeasons(ctx)
	if err != nil {
		return Child{}, err
	}
	c.Actions = familyActions(child, c.Memberships, seasons)
	c.EmergencyContacts, err = s.q.ListPersonEmergencyContacts(ctx, child)
	return c, err
}
func (s *Service) GetMyMembership(ctx context.Context, id int32) (Membership, error) {
	a, err := s.identity(ctx)
	if err != nil {
		return Membership{}, err
	}
	return s.membership(ctx, id, a.PersonID, a.PersonID)
}
func (s *Service) GetManagedChildMembership(ctx context.Context, child, id int32) (Membership, error) {
	a, err := s.authorizeChild(ctx, child)
	if err != nil {
		return Membership{}, err
	}
	return s.membership(ctx, id, child, a.PersonID)
}

// private: ownership is checked in SQL before any dependent facts are read.
func (s *Service) membership(ctx context.Context, id, person, viewer int32) (Membership, error) {
	r, err := s.q.GetPersonalMembership(ctx, dbsqlc.GetPersonalMembershipParams{MembershipID: id, PersonID: person})
	if err != nil {
		return Membership{}, unavailable(err)
	}
	m := Membership{Summary: Summary{ID: r.ID, SeasonName: r.SeasonName, MembershipTypeName: r.MembershipTypeName, Status: r.Status, Activities: r.Activities}, RequestedAt: r.RequestedAt.Time.In(s.location).Format("02/01/2006")}
	if r.JoinedAt.Valid {
		m.JoinedAt = r.JoinedAt.Time.Format("02/01/2006")
	}
	facts, err := s.q.ListMembershipCompletenessFacts(ctx, []int32{id})
	if err != nil {
		return Membership{}, err
	}
	if len(facts) != 1 {
		return Membership{}, ErrNotFound
	}
	now := time.Now().In(s.location)
	completeness := memberships.EvaluateCompleteness(facts[0], now)
	m.CanDecide = viewer != person || (completeness.IsMinor != nil && !*completeness.IsMinor)
	m.Complete = len(completeness.BlockingIssues) == 0
	for _, code := range completeness.BlockingIssues {
		if code == "minor_missing_emergency" {
			m.MissingEmergency = true
		}
	}
	consents, err := s.q.ListPersonalConsents(ctx, dbsqlc.ListPersonalConsentsParams{MembershipID: id, ViewerPersonID: viewer})
	if err != nil {
		return Membership{}, err
	}
	for _, c := range consents {
		at := ""
		if c.RecordedAt.Valid {
			at = c.RecordedAt.Time.In(s.location).Format("02/01/2006 à 15:04")
		}
		m.Consents = append(m.Consents, Consent{ID: c.ID, Editable: c.IsActive || c.Decision.String == "granted", Title: c.Title, Description: c.Description, Version: c.Version, Decision: c.Decision.String, RecordedAt: at, GivenByViewer: c.GivenByViewer})
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	groups, err := s.q.ListPersonalGroups(ctx, dbsqlc.ListPersonalGroupsParams{MembershipID: id, Today: dbtypes.Date{Time: today, Valid: true}})
	if err != nil {
		return Membership{}, err
	}
	for _, g := range groups {
		n := len(m.Groups) - 1
		if n < 0 || m.Groups[n].Name != g.GroupName || m.Groups[n].Activity != g.ActivityName {
			m.Groups = append(m.Groups, Group{Name: g.GroupName, Activity: g.ActivityName})
			n++
		}
		if g.Weekday.Valid {
			days := []string{"Lundi", "Mardi", "Mercredi", "Jeudi", "Vendredi", "Samedi", "Dimanche"}
			m.Groups[n].Slots = append(m.Groups[n].Slots, Slot{days[int(g.Weekday.Int16)-1], g.StartTime, g.EndTime, g.Location.String})
		}
	}
	return m, nil
}

// Open seasons come from the registration catalog. Every existing status blocks
// a second request; active and pending receive distinct, truthful next actions.
func familyActions(child int32, rows []Summary, seasons []dbsqlc.Season) []FamilyAction {
	actions := []FamilyAction{}
	for _, season := range seasons {
		a := FamilyAction{Season: season.Name, Label: "Demander une adhésion", URL: fmt.Sprintf("/me/children/%d/join?season=%d", child, season.ID)}
		for _, m := range rows {
			if m.SeasonID == season.ID {
				a.Label = "Voir le dossier"
				if m.Status == "active" {
					a.Label = "Voir l’adhésion"
				}
				a.URL = fmt.Sprintf("/me/children/%d/memberships/%d", child, m.ID)
				break
			}
		}
		actions = append(actions, a)
	}
	return actions
}

func (s *Service) HasContext(ctx context.Context) (bool, error) {
	actor, ok := auth.UserID(ctx)
	if !ok {
		return false, nil
	}
	has, err := s.q.HasOwnPersonalContext(ctx, actor)
	if err != nil || has.Bool {
		return has.Bool, err
	}
	children, err := s.guardians.ListManagedChildren(ctx)
	return len(children) > 0, err
}

func nullableIDs(ids []int32) []sql.NullInt32 {
	result := make([]sql.NullInt32, len(ids))
	for i, id := range ids {
		result[i] = sql.NullInt32{Int32: id, Valid: true}
	}
	return result
}

func (s *Service) today() dbtypes.Date {
	now := time.Now().In(s.location)
	return dbtypes.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

func (s *Service) canManageOwnEmergency(birth dbtypes.Date) bool {
	return !birth.Valid || !civildate.IsMinor(birth.Time, time.Now().In(s.location))
}
