package views

import (
	"embed"
	"html/template"
	"io"

	"github.com/grapinou/club-core/internal/useraccess"
)

type RoleOption struct {
	Name, Label string
	Assigned    bool
}

type UsersView struct {
	SecurityData
	SiteName, Title, Mode, Search, Error, Notice string
	Users                                        []useraccess.User
	User                                         useraccess.User
	Roles                                        []RoleOption
	Page                                         int32
	More                                         bool
}

//go:embed templates/layouts/base.html templates/pages/users.html
var usersFiles embed.FS
var usersTemplate = template.Must(template.ParseFS(usersFiles, "templates/layouts/base.html", "templates/pages/users.html"))

func RenderUsers(w io.Writer, v UsersView) error { return usersTemplate.ExecuteTemplate(w, "base", v) }

func RoleLabel(name string) string {
	switch name {
	case "president":
		return "Président"
	case "secretary":
		return "Secrétaire"
	case "treasurer":
		return "Trésorier"
	case "coach":
		return "Encadrant"
	default:
		return name
	}
}

func (v UsersView) Label(name string) string { return RoleLabel(name) }
func (v UsersView) NextPage() int32          { return v.Page + 1 }
func (v UsersView) PreviousPage() int32      { return v.Page - 1 }
