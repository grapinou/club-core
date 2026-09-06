package views

import (
	"embed"
	"html/template"
	"io"
)

//go:embed templates/layouts/base.html
//go:embed templates/pages/017_archived_persons_list.html
var archivedPersonsListFiles embed.FS

var archivedPersonsListTemplate = template.Must(
	template.ParseFS(
		archivedPersonsListFiles,
		"templates/layouts/base.html",
		"templates/pages/017_archived_persons_list.html",
	),
)

func RenderArchivedPersonsList(
	w io.Writer,
	data PersonsData,
) error {
	return archivedPersonsListTemplate.ExecuteTemplate(
		w,
		"base",
		data,
	)
}
