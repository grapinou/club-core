package views

import (
	"embed"
	"html/template"
	"io"
	"net/url"

	"github.com/grapinou/club-core/internal/registrationapplications"
)

type JoinHidden struct{ Name, Value string }
type JoinView struct {
	SecurityData
	SiteName, Title, Presentation  string
	Catalog                        registrationapplications.Catalog
	Values                         url.Values
	Errors                         map[string]string
	Review, Submitted, Unavailable bool
	Hidden                         []JoinHidden
}

func (v JoinView) Selected(name, value string) bool {
	for _, x := range v.Values[name] {
		if x == value {
			return true
		}
	}
	return false
}

//go:embed templates/layouts/base.html templates/pages/join.html
var joinFiles embed.FS
var joinTemplate = template.Must(template.ParseFS(joinFiles, "templates/layouts/base.html", "templates/pages/join.html"))

func RenderJoin(w io.Writer, v JoinView) error { return joinTemplate.ExecuteTemplate(w, "base", v) }
