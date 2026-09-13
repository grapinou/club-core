package views

import (
	"embed"
	"html/template"
	"io"
	"net/url"
	"strconv"

	"github.com/grapinou/club-core/internal/administration"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type AdministrativeView struct {
	SecurityData
	SiteName, Title, Mode, Error, Notice string
	Search, NextURL, PreviousURL         string
	More, CanManageMemberships           bool
	Home                                 administration.Home
	People                               []dbsqlc.SearchAdministrativePersonsRow
	Person                               administration.Person
	Trials                               []dbsqlc.AdministrativeTrialsRow
	Trial                                dbsqlc.AdministrativeTrialsRow
	Choices                              administration.Choices
	Membership                           administration.Membership
	Today                                pgtype.Date
	Form                                 url.Values
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
