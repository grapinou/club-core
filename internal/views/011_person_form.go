package views

import (
	"embed"
	"html/template"
	"io"
)

//go:embed templates/layouts/base.html
//go:embed templates/pages/011_person_form.html
var personFormFiles embed.FS

type PersonFormData struct {
	SiteName string
	Title    string
}

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
