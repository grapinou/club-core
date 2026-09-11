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
		}
		data := views.RegistrationVerifyView{SecurityData: pageSecurity(r), SiteName: h.siteName, Title: "Vérification de votre demande - " + h.siteName, Message: message}
		var buf bytes.Buffer
		err := views.RenderRegistrationVerify(&buf, data)
		writeMembershipPage(w, &buf, err)
	}))
	mux.Handle("POST /registration/verify", h.sensitive(func(w http.ResponseWriter, r *http.Request) {
		result := "verified"
		if err := v.VerifyEmail(r.Context(), strings.TrimSpace(r.PostForm.Get("reference")), strings.TrimSpace(r.PostForm.Get("code"))); err != nil {
			result = "failed"
		}
		http.Redirect(w, r, "/registration/verify?result="+result, http.StatusSeeOther)
	}))
}
