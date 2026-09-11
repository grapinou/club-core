package router

import (
	"net/http"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/handlers"
	"github.com/grapinou/club-core/internal/websecurity"
)

func New(cfg config.Config, queries database.Queries, access *handlers.Access, csrf *websecurity.CSRF) *http.ServeMux {

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", handlers.HomeHandler(cfg))
	mux.HandleFunc("GET /club", handlers.ClubHandler(cfg))
	mux.HandleFunc("GET /contact", handlers.ContactHandler(cfg))
	mux.HandleFunc("GET /where", handlers.WhereHandler(cfg))
	mux.HandleFunc("GET /when", handlers.WhenHandler(cfg))
	mux.HandleFunc("GET /rules", handlers.RulesHandler(cfg))

	mux.Handle("GET /persons", access.RequirePermission(authorization.PersonsRead, csrf.Protect(handlers.PersonsListHandler(cfg, queries))))
	mux.Handle("GET /persons/new", access.RequirePermission(authorization.PersonsWrite, csrf.Protect(handlers.PersonFormHandler(cfg))))
	mux.Handle("POST /persons", access.RequirePermission(authorization.PersonsWrite, csrf.Protect(handlers.PostPersonHandler(queries))))
	mux.Handle("GET /persons/{id}/edit", access.RequirePermission(authorization.PersonsWrite, csrf.Protect(handlers.UpdatePersonFormHandler(cfg, queries))))
	mux.Handle("POST /persons/{id}/edit", access.RequirePermission(authorization.PersonsWrite, csrf.Protect(handlers.PostUpdatePersonHandler(queries))))
	mux.Handle("POST /persons/{id}/archive", access.RequirePermission(authorization.PersonsWrite, csrf.Protect(handlers.PostArchivePersonHandler(queries))))
	mux.Handle("GET /persons/archived", access.RequirePermission(authorization.PersonsRead, csrf.Protect(handlers.ArchivedPersonsListHandler(cfg, queries))))
	mux.Handle("POST /persons/{id}/restore", access.RequirePermission(authorization.PersonsWrite, csrf.Protect(handlers.PostRestorePersonHandler(queries))))

	staticFiles := http.FileServer(http.Dir("static"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFiles))

	return mux

}
