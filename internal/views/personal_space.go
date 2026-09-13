package views

import (
	"embed"
	"html/template"
	"io"

	"github.com/grapinou/club-core/internal/personalspace"
)

// Dedicated personal views accept only minimized service DTOs, never admin data.
type PersonalAccountView struct{ personalspace.Account }
type ChildOverviewView struct{ personalspace.Child }
type PersonalMembershipView struct {
	personalspace.Membership
	ChildID int32
}
type PersonalPage struct {
	SecurityData
	SiteName, Title string
	Account         *PersonalAccountView
	Child           *ChildOverviewView
	Membership      *PersonalMembershipView
}

//go:embed templates/layouts/base.html templates/pages/personal_space.html
var personalFiles embed.FS
var personalTemplate = template.Must(template.ParseFS(personalFiles, "templates/layouts/base.html", "templates/pages/personal_space.html"))

func RenderPersonal(w io.Writer, v PersonalPage) error {
	return personalTemplate.ExecuteTemplate(w, "base", v)
}
