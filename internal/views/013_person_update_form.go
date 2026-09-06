package views

import (
	"embed"
	"html/template"
	"io"
)

//go:embed templates/layouts/base.html
//go:embed templates/pages/013_person_update_form.html
var personUpdateFormFiles embed.FS

type PersonUpdateFormData struct {
	ID          int32
	FirstName   string
	LastName    string
	BirthDate   string
	PhoneNumber string
	Email       string
	Address     string
}

type PersonUpdateFormPageData struct {
	SiteName string
	Title    string
	Person   PersonUpdateFormData
}

var personUpdateFormTemplate = template.Must(
	template.ParseFS(
		personUpdateFormFiles,
		"templates/layouts/base.html",
		"templates/pages/013_person_update_form.html",
	),
)

func RenderPersonUpdateForm(w io.Writer, data PersonUpdateFormPageData) error {
	return personUpdateFormTemplate.ExecuteTemplate(
		w,
		"base",
		data,
	)
}
