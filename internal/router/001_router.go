package router

import (
	"net/http"

	"github.com/grapinou/club-manager/internal/config"
	"github.com/grapinou/club-manager/internal/database"
	"github.com/grapinou/club-manager/internal/handlers"
)

func New(cfg config.Config, queries database.Queries) *http.ServeMux {

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", handlers.HomeHandler(cfg))
	mux.HandleFunc("GET /club", handlers.ClubHandler(cfg))
	mux.HandleFunc("GET /contact", handlers.ContactHandler(cfg))
	mux.HandleFunc("GET /where", handlers.WhereHandler(cfg))
	mux.HandleFunc("GET /when", handlers.WhenHandler(cfg))
	mux.HandleFunc("GET /rules", handlers.RulesHandler(cfg))

	mux.HandleFunc("GET /persons", handlers.PersonsListHandler(cfg, queries))
	mux.HandleFunc("GET /persons/new", handlers.PersonFormHandler(cfg))
	mux.HandleFunc("POST /persons", handlers.PostPersonHandler(queries))
	mux.HandleFunc("GET /persons/{id}/edit", handlers.UpdatePersonFormHandler(cfg, queries))
	mux.HandleFunc("POST /persons/{id}/edit", handlers.PostUpdatePersonHandler(queries))
	mux.HandleFunc("POST /persons/{id}/archive", handlers.PostArchivePersonHandler(queries))
	mux.HandleFunc("GET /persons/archived", handlers.ArchivedPersonsListHandler(cfg, queries))
	mux.HandleFunc("POST /persons/{id}/restore", handlers.PostRestorePersonHandler(queries))

	staticFiles := http.FileServer(http.Dir("static"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFiles))

	return mux

}
