package handlers

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/grapinou/club-core/internal/views"
)

type EmailVerifier interface {
	VerifyEmail(context.Context, string, string) error
}

// Reuses auth's shared IP/global budget, CSRF, origin checks and body limit.
func (h *AuthHandler) RegisterRegistrationVerification(mux *http.ServeMux, v EmailVerifier) {
	mux.Handle("GET /registration/verify", h.sensitive(func(w http.ResponseWriter, r *http.Request) {
		message := ""
		switch r.URL.Query().Get("result") {
		case "failed":
			message = "Impossible de vérifier cette demande avec ces informations."
		case "verified":
			message = "Votre identité a été vérifiée."
		case "membership":
			message = "Votre identité a été vérifiée et votre demande d’adhésion a été enregistrée."
		case "review":
			message = "Votre identité a été vérifiée. Le club doit effectuer une vérification complémentaire de votre demande."
		}
		data := views.RegistrationVerifyView{SecurityData: pageSecurity(r), SiteName: h.siteName, Title: "Vérification de votre demande - " + h.siteName, Message: message}
		var buf bytes.Buffer
		err := views.RenderRegistrationVerify(&buf, data)
		writeMembershipPage(w, &buf, err)
	}))
	mux.Handle("POST /registration/verify", h.sensitive(func(w http.ResponseWriter, r *http.Request) {
		result := "verified"
		reference, code := strings.TrimSpace(r.PostForm.Get("reference")), strings.TrimSpace(r.PostForm.Get("code"))
		if enhanced, ok := v.(interface {
			VerifyEmailOutcome(context.Context, string, string) (string, error)
		}); ok {
			var err error
			result, err = enhanced.VerifyEmailOutcome(r.Context(), reference, code)
			if err != nil {
				result = "failed"
			}
		} else if err := v.VerifyEmail(r.Context(), reference, code); err != nil {
			result = "failed"
		}
		http.Redirect(w, r, "/registration/verify?result="+result, http.StatusSeeOther)
	}))
}
