package views

import (
	"embed"
	"fmt"
	"html/template"
	"io"

	"github.com/grapinou/club-core/internal/personalspace"
)

type DashboardView struct {
	SecurityData
	personalspace.Dashboard
	SiteName, Title string
}

// FamilyAction remains authoritative for open seasons. Keep the membership link
// available even when its season is no longer open, without duplicating an action.
func (DashboardView) ChildActions(child personalspace.ChildSummary) []personalspace.FamilyAction {
	if len(child.Memberships) == 0 {
		return child.Actions
	}
	current := child.Memberships[0]
	url := fmt.Sprintf("/me/children/%d/memberships/%d", child.ID, current.ID)
	for _, action := range child.Actions {
		if action.URL == url {
			return child.Actions
		}
	}
	return append(append([]personalspace.FamilyAction{}, child.Actions...), personalspace.FamilyAction{Season: current.SeasonName, Label: "Voir l’adhésion", URL: url})
}

//go:embed templates/layouts/base.html templates/pages/dashboard.html
var dashboardFiles embed.FS
var dashboardTemplate = template.Must(template.New("dashboard").Funcs(template.FuncMap{"DisplayLabel": func(code string) string { return DisplayStatus(code).Label }, "DisplayClass": func(code string) string { return DisplayStatus(code).Class }}).ParseFS(dashboardFiles, "templates/layouts/base.html", "templates/pages/dashboard.html"))

func RenderDashboard(w io.Writer, v DashboardView) error {
	return dashboardTemplate.ExecuteTemplate(w, "base", v)
}
