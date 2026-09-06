package views

import (
	"embed"
	"html/template"
	"io"
)

// pageFiles contient les fichiers intégrés correspondant
// au template de la page d'accueil.

//go:embed templates/layouts/base.html
//go:embed templates/pages/000_page.html
var pageFiles embed.FS

type PageData struct {
	SiteName    string
	Title       string
	Heading     string
	Description string
	Image       string
	ImageAlt    string
}

// pageTemplate représente le fichier HTML analysé par Go.
//
// template.Must arrête immédiatement le programme si le template
// contient une erreur de syntaxe.
var pageTemplate = template.Must(
	template.ParseFS(pageFiles,
		"templates/layouts/base.html",
		"templates/pages/000_page.html",
	),
)

// RenderPage exécute le template de la page d'accueil
// et écrit le résultat dans la destination reçue.
// donne le nom défni par {{ define }} et non le nom du fichier
func RenderPage(w io.Writer, data PageData) error {
	return pageTemplate.ExecuteTemplate(
		w,
		"base",
		data,
	)
}
