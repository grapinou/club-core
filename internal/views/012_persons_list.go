package views

import (
	"embed"
	"html/template"
	"io"
)

//go:embed templates/layouts/base.html
//go:embed templates/pages/012_persons_list.html
var personsListFiles embed.FS

type PersonData struct {
	ID          int32
	FirstName   string
	LastName    string
	BirthDate   string
	PhoneNumber string
	Email       string
	Address     string
	CreatedAt   string
}

type PersonsData struct {
	SiteName string
	Title    string
	Persons  []PersonData
}

var personsListTemplate = template.Must(
	template.ParseFS(
		personsListFiles,
		"templates/layouts/base.html",
		"templates/pages/012_persons_list.html",
	),
)

func RenderPersonsList(w io.Writer, data PersonsData) error {
	return personsListTemplate.ExecuteTemplate(
		w,
		"base",
		data,
	)
}
