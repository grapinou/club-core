package views

import (
	"embed"
	"html/template"
	"io"
	"net/url"
	"strconv"
	"time"

	"github.com/grapinou/club-core/internal/administration"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/trials"
	"github.com/jackc/pgx/v5/pgtype"
)

type AdministrativeView struct {
	EligibleTrials                            []dbsqlc.EligibleMembershipSourceTrialsRow
	ListPath, SearchPath                      string
	TrialPolicy                               pgtype.Int4
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
	Today                                pgtype.Date
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
func (v AdministrativeView) Current(joined, left pgtype.Date) bool {
	return !joined.Time.After(v.Today.Time) && (!left.Valid || left.Time.After(v.Today.Time))
}
func (v AdministrativeView) Past(d pgtype.Date) bool {
	return d.Valid && d.Time.Before(v.Today.Time)
}

//go:embed templates/layouts/base.html templates/pages/administration.html
var administrativeFiles embed.FS
var administrativeTemplate = template.Must(template.New("administration").Funcs(template.FuncMap{"date": date, "iso": func(d pgtype.Date) string {
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
		return "Personnes ayant une adhésion active dans une saison active qui couvre la date d’aujourd’hui. Les adhésions historiques restent dans Adhésions."
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
