package views

import (
	"embed"
	"html/template"
	"io"
	"net/url"
)

//go:embed templates/layouts/base.html
//go:embed templates/pages/011_person_form.html
var personFormFiles embed.FS

type PersonFormData struct {
	AfterTrial      bool
	AfterMembership bool
	SecurityData
	SiteName string
	Title    string
	Error    string
	Values   url.Values
}

func (v PersonFormData) V(key string) string { return v.Values.Get(key) }

var personFormTemplate = template.Must(
	template.ParseFS(
		personFormFiles,
		"templates/layouts/base.html",
		"templates/pages/011_person_form.html",
	),
)

func RenderPersonForm(w io.Writer, data PersonFormData) error {
	return personFormTemplate.ExecuteTemplate(
		w,
		"base",
		data,
	)
}
