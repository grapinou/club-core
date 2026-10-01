package views

import (
	"database/sql"
	"embed"
	"html/template"
	"io"
	"net/url"
	"strconv"
	"time"

	"github.com/grapinou/club-core/internal/activation"
	"github.com/grapinou/club-core/internal/administration"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/database/dbtypes"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/trials"
)

type TrialDay struct {
	Name, Date string
	Trials     []dbsqlc.AdministrativeTrialsRow
}
type AdministrativeView struct {
	WeekDays                                  []TrialDay
	WeekLabel, PreviousWeek, NextWeek         string
	EligibleTrials                            []dbsqlc.EligibleMembershipSourceTrialsRow
	ListPath, SearchPath                      string
	TrialPolicy                               sql.NullInt32
	PendingMemberships, ActivationMemberships []MembershipRowView
	TrialQuota                                []trials.QuotaUsage
	SecurityData
	SiteName, Title, Mode, Error, Notice string
	Category                             string
	Search, NextURL, PreviousURL         string
	More, CanManageMemberships           bool
	Home                                 administration.Home
	People                               []dbsqlc.SearchAdministrativePersonsRow
	Person                               administration.Person
	Trials                               []dbsqlc.AdministrativeTrialsRow
	Trial                                dbsqlc.AdministrativeTrialsRow
	TrialGuardians                       []dbsqlc.AdministrativeRelationsRow
	Choices                              administration.Choices
	Membership                           administration.Membership
	Today                                dbtypes.Date
	Form                                 url.Values
}

func (v *AdministrativeView) SetMembershipAttention(entries []memberships.ListEntry, loc *time.Location) {
	for _, row := range MembershipRows(entries, loc) {
		if row.Pending {
			v.PendingMemberships = append(v.PendingMemberships, row)
		} else if row.NeedsActivation {
			v.ActivationMemberships = append(v.ActivationMemberships, row)
		}
	}
}

func (AdministrativeView) Preview(rows []MembershipRowView) []MembershipRowView {
	if len(rows) > 5 {
		return rows[:5]
	}
	return rows
}

func (v AdministrativeView) V(key string) string { return v.Form.Get(key) }
func (v AdministrativeView) Selected(key string, id int32) bool {
	for _, value := range v.Form[key] {
		if value == strconv.FormatInt(int64(id), 10) {
			return true
		}
	}
	return false
}
func (v AdministrativeView) Current(joined, left dbtypes.Date) bool {
	return !joined.Time.After(v.Today.Time) && (!left.Valid || left.Time.After(v.Today.Time))
}
func (v AdministrativeView) Past(d dbtypes.Date) bool {
	return d.Valid && d.Time.Before(v.Today.Time)
}

//go:embed templates/layouts/base.html templates/pages/administration.html
var administrativeFiles embed.FS
var administrativeTemplate = template.Must(template.New("administration").Funcs(template.FuncMap{"date": date, "iso": func(d dbtypes.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}, "day": func(d int16) string {
	if d < 1 || d > 7 {
		return ""
	}
	return []string{"Lundi", "Mardi", "Mercredi", "Jeudi", "Vendredi", "Samedi", "Dimanche"}[d-1]
}}).ParseFS(administrativeFiles, "templates/layouts/base.html", "templates/pages/administration.html"))

func RenderAdministrative(w io.Writer, v AdministrativeView) error {
	return administrativeTemplate.ExecuteTemplate(w, "base", v)
}
func (AdministrativeView) TrialStatuses() []string {
	return []string{"registered", "attended", "cancelled", "no_show"}
}

func (v AdministrativeView) PeopleTitle() string {
	switch v.Category {
	case "members":
		return "Membres actuels"
	case "guardians":
		return "Responsables"
	case "prospects":
		return "Prospects après essai"
	case "memberships":
		return "Personnes ayant un dossier d’adhésion"
	default:
		return "Annuaire"
	}
}
func (v AdministrativeView) PeopleDescription() string {
	switch v.Category {
	case "members":
		return "Un membre est une personne dont l’adhésion est active dans une saison active couvrant la date d’aujourd’hui."
	case "guardians":
		return "Personnes liées à au moins un enfant comme responsable. Une relation ne signifie pas qu’un accès familial est autorisé."
	case "prospects":
		return "Personnes ayant un essai enregistré et aucun dossier d’adhésion, même historique. Les contacts sans essai sont dans l’Annuaire."
	case "memberships":
		return "Personnes ayant un dossier d’adhésion, quel que soit son état ou sa saison."
	default:
		return "Tous les contacts non archivés du club, y compris ceux sans essai ni adhésion."
	}
}

func (v *AdministrativeView) SetWeek(start time.Time) {
	v.WeekLabel = start.Format("02/01/2006") + " – " + start.AddDate(0, 0, 6).Format("02/01/2006")
	v.PreviousWeek = start.AddDate(0, 0, -7).Format("2006-01-02")
	v.NextWeek = start.AddDate(0, 0, 7).Format("2006-01-02")
	for i, name := range []string{"Lundi", "Mardi", "Mercredi", "Jeudi", "Vendredi", "Samedi", "Dimanche"} {
		d := start.AddDate(0, 0, i)
		day := TrialDay{Name: name, Date: d.Format("02/01")}
		for _, trial := range v.Trials {
			if trial.TrialDate.Time.Equal(d) {
				day.Trials = append(day.Trials, trial)
			}
		}
		v.WeekDays = append(v.WeekDays, day)
	}
}

func (v AdministrativeView) ActivityGroups(activity int32) []dbsqlc.ListActiveGroupsRow {
	var out []dbsqlc.ListActiveGroupsRow
	for _, g := range v.Choices.Groups {
		if g.ActivityID == activity && v.GroupCompatible(g.ID) {
			out = append(out, g)
		}
	}
	return out
}
func (v AdministrativeView) DefaultActivityGroup(activity, group int32) bool {
	key := "group-" + strconv.Itoa(int(activity))
	if values, ok := v.Form[key]; ok {
		return len(values) > 0 && values[0] == strconv.Itoa(int(group))
	}
	groups := v.ActivityGroups(activity)
	return len(groups) == 1 && groups[0].ID == group
}

func (AdministrativeView) UsableEmail(email string) bool { return activation.UsableEmail(email) }

func (v AdministrativeView) PersonHasActivationEmail() bool {
	if activation.UsableEmail(v.Person.Info.Email.String) {
		return true
	}
	for _, g := range v.Person.Relations {
		if g.IsGuardian && activation.UsableEmail(g.Email.String) {
			return true
		}
	}
	return false
}

func (v AdministrativeView) GroupCompatible(group int32) bool {
	kind := v.Membership.Info.MembershipTypeID
	if v.Mode == "membership-new" {
		n, _ := strconv.ParseInt(v.Form.Get("type_id"), 10, 32)
		kind = int32(n)
	}
	for _, c := range v.Choices.Compatibility {
		if c.MembershipTypeID == kind && c.GroupID == group {
			return true
		}
	}
	return false
}

func (v AdministrativeView) ActivityName(activity int32) string {
	for _, a := range v.Choices.Activities {
		if a.ID == activity {
			return a.Name
		}
	}
	return "Activité"
}
func (v AdministrativeView) HasCurrentActivityGroup(activity int32) bool {
	for _, g := range v.Membership.History {
		if g.ActivityID == activity && !g.LeftAt.Valid {
			return true
		}
	}
	return false
}
func (v AdministrativeView) ProposedMembershipGroup(activity, group int32) bool {
	if _, ok := v.Form["group_id"]; ok {
		return v.Selected("group_id", group)
	}
	groups := v.ActivityGroups(activity)
	return !v.HasCurrentActivityGroup(activity) && len(groups) == 1 && groups[0].ID == group
}
