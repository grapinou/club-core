package handlers

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/identitycorrections"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

func RegisterIdentityCorrections(mux *http.ServeMux, site string, s *identitycorrections.Service, loc *time.Location, access *Access, csrf *websecurity.CSRF) {
	render := func(w http.ResponseWriter, r *http.Request, v views.IdentityCorrectionView, status int) {
		v.SecurityData = pageSecurity(r)
		v.SiteName = site
		v.Title = "Correction d’identité - " + site
		var body bytes.Buffer
		if err := views.RenderIdentityCorrection(&body, v); err != nil {
			personalError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(body.Bytes())
	}
	personal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, err := s.Current(r.Context())
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			personalError(w, r, err)
			return
		}
		v := views.IdentityCorrectionView{Mode: "personal", FirstName: a.FirstName, LastName: a.LastName, Pending: a.IdentityCorrectionPending, Errors: map[string]string{}}
		if a.BirthDate.Valid {
			v.BirthDate = a.BirthDate.Time.Format("2006-01-02")
		}
		status := 200
		if r.Method == http.MethodPost {
			v.FirstName, v.LastName, v.BirthDate = r.PostForm.Get("first_name"), r.PostForm.Get("last_name"), r.PostForm.Get("birth_date")
			err = s.Request(r.Context(), v.FirstName, v.LastName, v.BirthDate)
			if err == nil {
				http.Redirect(w, r, "/me/account/identity?sent=1", 303)
				return
			}
			var fields identitycorrections.Fields
			switch {
			case errors.As(err, &fields):
				v.Errors = fields
				v.Error = "Vérifiez les champs indiqués."
				status = 422
			case errors.Is(err, identitycorrections.ErrPending):
				v.Pending = true
				status = 409
			case errors.Is(err, sql.ErrNoRows):
				http.NotFound(w, r)
				return
			default:
				v.Error = "La demande est temporairement indisponible."
				status = 503
			}
		} else if r.URL.Query().Get("sent") == "1" && v.Pending {
			v.Message = "Votre demande de correction a été envoyée au club. Votre identité actuelle reste inchangée."
		}
		render(w, r, v, status)
	})
	for _, method := range []string{"GET", "POST"} {
		mux.Handle(method+" /me/account/identity", RequireAuthenticated(csrf.Protect(personal)))
	}
	list := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows, err := s.List(r.Context())
		if err != nil {
			correctionError(w, r, err)
			return
		}
		render(w, r, views.IdentityCorrectionView{Mode: "list", Rows: views.CorrectionRows(rows, loc)}, 200)
	})
	detail := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := adminID(r, "id")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		status := 200
		message := ""
		if r.Method == http.MethodPost {
			err = s.Review(r.Context(), id, r.PostForm.Get("decision"))
			if err == nil {
				http.Redirect(w, r, fmt.Sprintf("/identity-corrections/%d?saved=1", id), 303)
				return
			}
			var fields identitycorrections.Fields
			switch {
			case errors.Is(err, identitycorrections.ErrClosed):
				message = "Cette demande a déjà été traitée."
				status = 409
			case errors.Is(err, identitycorrections.ErrChanged):
				message = "L’identité de référence a changé ou la fiche est archivée. Refusez cette demande pour permettre une nouvelle proposition."
				status = 409
			case errors.As(err, &fields):
				message = "Vérifiez les informations de la demande."
				status = 422
			default:
				correctionError(w, r, err)
				return
			}
		}
		row, err := s.Get(r.Context(), id)
		if err != nil {
			correctionError(w, r, err)
			return
		}
		v := views.CorrectionDetail(row, loc)
		v.Error = message
		render(w, r, v, status)
	})
	mux.Handle("GET /identity-corrections", access.RequirePermission(authorization.RegistrationsReview, csrf.Protect(list)))
	for _, method := range []string{"GET", "POST"} {
		mux.Handle(method+" /identity-corrections/{id}", access.RequirePermission(authorization.RegistrationsReview, csrf.Protect(detail)))
	}
}
func correctionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, authorization.ErrForbidden):
		http.Error(w, "Accès refusé", 403)
	case errors.Is(err, sql.ErrNoRows):
		http.NotFound(w, r)
	default:
		http.Error(w, "Les corrections sont temporairement indisponibles.", 503)
	}
}
