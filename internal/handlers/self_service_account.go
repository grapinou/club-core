package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"

	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/personalspace"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

func RegisterSelfServiceAccount(mux *http.ServeMux, site string, s *accounts.SelfService, personal *personalspace.Service, sessions *auth.Sessions, csrf *websecurity.CSRF) {
	limiter := NewAttemptLimiter()
	for _, kind := range []string{"profile", "email", "email/verify", "password"} {
		path := "/me/account/" + kind
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v := views.AccountFormView{SecurityData: pageSecurity(r), SiteName: site, Title: "Mon compte - " + site, Kind: kind, Errors: map[string]string{}}
			status := http.StatusOK
			if r.Method == http.MethodPost {
				id, _ := auth.UserID(r.Context())
				if kind != "profile" && !limiter.Allow(fmt.Sprintf("account-%d-%s", id, kind)) {
					w.Header().Set("Retry-After", "900")
					http.Error(w, "Trop de tentatives. Réessayez plus tard.", http.StatusTooManyRequests)
					return
				}
				var err error
				switch kind {
				case "profile":
					v.Phone = r.PostForm.Get("phone_number")
					v.Address = r.PostForm.Get("address")
					err = s.UpdateContact(r.Context(), v.Phone, v.Address)
				case "email":
					v.Email = r.PostForm.Get("new_email")
					err = s.RequestEmail(r.Context(), v.Email, r.PostForm.Get("current_password"))
				case "email/verify":
					err = s.VerifyEmail(r.Context(), r.PostForm.Get("code"))
				case "password":
					var credential [32]byte
					credential, err = s.ChangePassword(r.Context(), r.PostForm.Get("current_password"), r.PostForm.Get("new_password"), r.PostForm.Get("confirmation"))
					if err == nil {
						if rotateErr := sessions.RotateAccount(w, r, id, credential); rotateErr != nil {
							sessions.Delete(w, r)
							http.Redirect(w, r, "/login", http.StatusSeeOther)
							return
						}
					}
				}
				if err == nil {
					target := "/me/account/" + kind + "?saved=1"
					if kind == "email" {
						target = "/me/account/email/verify?sent=1"
					}
					http.Redirect(w, r, target, http.StatusSeeOther)
					return
				}
				var fields accounts.AccountFields
				switch {
				case errors.As(err, &fields):
					v.Errors = fields
					status = http.StatusUnprocessableEntity
				case errors.Is(err, accounts.ErrEmailChangeLimited):
					v.Error = "Trop de demandes. Réessayez dans une heure."
					status = 429
					w.Header().Set("Retry-After", "3600")
				case errors.Is(err, accounts.ErrEmailChangeDelivery):
					v.Error = "L’envoi du code est temporairement indisponible. Votre adresse actuelle reste inchangée."
					status = 503
				case errors.Is(err, accounts.ErrSelfServiceUnavailable):
					http.NotFound(w, r)
					return
				default:
					v.Error = "Votre compte est temporairement indisponible."
					status = 503
				}
			} else {
				if kind == "profile" {
					a, err := personal.GetMyAccount(r.Context())
					if err != nil {
						personalError(w, r, err)
						return
					}
					v.Phone = a.Phone
					v.Address = a.Address
				}
				if r.URL.Query().Get("saved") == "1" {
					v.Message = map[string]string{"profile": "Vos coordonnées ont été mises à jour.", "email/verify": "Votre nouvelle adresse email a été vérifiée.", "password": "Votre mot de passe a été modifié."}[kind]
				}
				if kind == "email/verify" && r.URL.Query().Get("sent") == "1" {
					v.Message = "Un code de vérification a été envoyé à la nouvelle adresse."
				}
			}
			var body bytes.Buffer
			if err := views.RenderAccountForm(&body, v); err != nil {
				personalError(w, r, err)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(status)
			_, _ = w.Write(body.Bytes())
		})
		for _, method := range []string{"GET", "POST"} {
			mux.Handle(method+" "+path, RequireAuthenticated(csrf.Protect(handler)))
		}
	}
}
