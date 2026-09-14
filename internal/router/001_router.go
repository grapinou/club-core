package router

import (
	"net/http"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/handlers"
	"github.com/grapinou/club-core/internal/websecurity"
)

// New preserves the standalone legacy router API. Production uses NewWithPublic.
func New(cfg config.Config, queries database.Queries, access *handlers.Access, csrf *websecurity.CSRF, personLists ...http.Handler) *http.ServeMux {
	return newRouter(cfg, queries, access, csrf, nil, personLists...)
}
func NewWithPublic(cfg config.Config, queries database.Queries, access *handlers.Access, csrf *websecurity.CSRF, public http.Handler, personList http.Handler) *http.ServeMux {
	return newRouter(cfg, queries, access, csrf, public, personList)
}
func newRouter(cfg config.Config, queries database.Queries, access *handlers.Access, csrf *websecurity.CSRF, public http.Handler, personLists ...http.Handler) *http.ServeMux {

	mux := http.NewServeMux()

	if public != nil {
		public = csrf.Protect(public)
		mux.Handle("GET /{$}", public)
		for _, path := range []string{"/horaires", "/tarifs", "/contact", "/essai", "/rules"} {
			mux.Handle("GET "+path, public)
		}
		for from, to := range map[string]string{"/club": "/", "/where": "/contact#lieux", "/when": "/horaires"} {
			mux.Handle("GET "+from, http.RedirectHandler(to, http.StatusPermanentRedirect))
		}
	} else {
		mux.HandleFunc("GET /{$}", handlers.HomeHandler(cfg))
		mux.HandleFunc("GET /club", handlers.ClubHandler(cfg))
		mux.HandleFunc("GET /contact", handlers.ContactHandler(cfg))
		mux.HandleFunc("GET /where", handlers.WhereHandler(cfg))
		mux.HandleFunc("GET /when", handlers.WhenHandler(cfg))
		mux.HandleFunc("GET /rules", handlers.RulesHandler(cfg))
	}

	var personList http.Handler = handlers.PersonsListHandler(cfg, queries)
	if len(personLists) > 0 {
		personList = personLists[0]
	}
	mux.Handle("GET /persons", access.RequirePermission(authorization.PersonsRead, csrf.Protect(personList)))
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
