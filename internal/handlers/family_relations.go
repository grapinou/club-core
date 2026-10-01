package handlers

import (
	"bytes"
	"database/sql"
	"errors"
	"net/http"

	"github.com/grapinou/club-core/internal/administration"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/guardianaccess"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
	"modernc.org/sqlite"
)

func (h *AdministrativeHandler) RegisterFamilyRelations(mux *http.ServeMux, access *Access, csrf *websecurity.CSRF, g *guardianaccess.Service) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := adminID(r, "id")
		v := views.FamilyView{SecurityData: pageSecurity(r), SiteName: h.site, Title: "Responsables - " + h.site}
		status := 200
		if err == nil {
			v.Person, err = h.s.Person(r.Context(), id)
		}
		if err == nil && !v.Person.IsMinor {
			http.NotFound(w, r)
			return
		}
		if err == nil && r.Method == "POST" {
			action := r.PostForm.Get("action")
			if action == "search" {
				v.Search = r.PostForm.Get("search")
				if v.Search != "" {
					v.Candidates, err = h.s.People(r.Context(), v.Search, 0, "")
				}
			} else {
				var guardian int32
				guardian, err = formID(r.PostForm, "guardian_id", false)
				if err == nil {
					switch action {
					case "add", "update":
						err = h.s.SaveGuardian(r.Context(), id, guardian, r.PostForm.Get("relationship"), r.PostForm.Get("primary") == "yes", action == "add")
					case "remove":
						err = g.RemoveRelationship(r.Context(), id, guardian)
					default:
						err = authorization.ErrForbidden
					}
				}
				if err == nil {
					http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
					return
				}
			}
		}
		if err != nil {
			if errors.Is(err, authorization.ErrForbidden) {
				http.Error(w, "Accès refusé", 403)
				return
			}
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			var dbErr *sqlite.Error
			if !errors.Is(err, administration.ErrInvalid) && !(errors.As(err, &dbErr) && (dbErr.Code() == 2067 || dbErr.Code() == 275 || dbErr.Code() == 787)) {
				http.Error(w, "Les responsables sont temporairement indisponibles.", 503)
				return
			}
			status = 422
			v.Error = "Vérifiez la personne choisie et le lien familial. Cette relation peut déjà exister."
		}
		var body bytes.Buffer
		if e := views.RenderFamily(&body, v); e != nil {
			http.Error(w, "Page temporairement indisponible.", 503)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(body.Bytes())
	})
	for _, method := range []string{"GET", "POST"} {
		mux.Handle(method+" /persons/{id}/family", access.RequirePermission(authorization.PersonsWrite, csrf.Protect(handler)))
	}
}
