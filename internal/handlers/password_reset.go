package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/passwordreset"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

const recoveryNotice = "Si ce compte existe et peut recevoir un email, un lien de réinitialisation a été envoyé."
const invalidResetNotice = "Ce lien est invalide ou n’est plus utilisable. Demandez un nouveau lien de réinitialisation."

type PasswordResetService interface {
	Submit(string)
	Validate(context.Context, string) error
	Reset(context.Context, string, string, string) error
	RequestForPerson(context.Context, int32) error
}

type PasswordResetHandler struct {
	site                   string
	s                      PasswordResetService
	requests, consumptions *AttemptLimiter
}

func NewPasswordResetHandler(site string, s PasswordResetService) *PasswordResetHandler {
	return &PasswordResetHandler{site: site, s: s, requests: NewPasswordRecoveryLimiter(), consumptions: NewAttemptLimiter()}
}

func (h *PasswordResetHandler) Register(mux *http.ServeMux, access *Access, csrf *websecurity.CSRF) {
	mux.Handle("GET /password/forgot", csrf.Protect(http.HandlerFunc(h.forgot)))
	mux.Handle("POST /password/forgot", csrf.Protect(http.HandlerFunc(h.forgot)))
	mux.Handle("GET /password/reset", csrf.Protect(http.HandlerFunc(h.reset)))
	mux.Handle("POST /password/reset", csrf.Protect(http.HandlerFunc(h.reset)))
	mux.Handle("POST /persons/{id}/password-reset", access.RequirePermission(authorization.PasswordReset, csrf.Protect(http.HandlerFunc(h.administrative))))
}

func (h *PasswordResetHandler) render(w http.ResponseWriter, r *http.Request, v views.PasswordResetData, status int) {
	v.SecurityData = pageSecurity(r)
	v.SiteName = h.site
	v.Title = "Mot de passe oublié - " + h.site
	if v.Reset {
		v.Title = "Réinitialiser votre mot de passe - " + h.site
	}
	var b bytes.Buffer
	if err := views.RenderPasswordReset(&b, v); err != nil {
		http.Error(w, "Page temporairement indisponible.", 503)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}

func (h *PasswordResetHandler) forgot(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// IP/global overload, unknown users and account budgets share this response.
		if h.requests.AllowRequest(r) {
			h.s.Submit(r.PostForm.Get("username"))
		}
		http.Redirect(w, r, "/password/forgot?sent=1", 303)
		return
	}
	v := views.PasswordResetData{Sent: r.URL.Query().Get("sent") == "1"}
	if v.Sent {
		v.Message = recoveryNotice
	}
	h.render(w, r, v, 200)
}

func (h *PasswordResetHandler) reset(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if r.Method == http.MethodPost {
		token = r.PostForm.Get("token")
		if !h.consumptions.AllowRequest(r) {
			w.Header().Set("Retry-After", "900")
			http.Error(w, "Trop de tentatives. Réessayez plus tard.", 429)
			return
		}
	}
	err := h.s.Validate(r.Context(), token)
	if err == nil && r.Method == http.MethodPost {
		err = h.s.Reset(r.Context(), token, r.PostForm.Get("password"), r.PostForm.Get("confirmation"))
		if err == nil {
			http.Redirect(w, r, "/login?password_reset=1", 303)
			return
		}
	}
	v := views.PasswordResetData{Reset: true, Token: token}
	status := 200
	switch {
	case errors.Is(err, passwordreset.ErrInvalidToken):
		v.Invalid = true
		v.Error = true
		v.Token = ""
		v.Message = invalidResetNotice
		status = 422
	case errors.Is(err, passwordreset.ErrPassword):
		v.Error = true
		v.Message = "Le mot de passe doit contenir de 12 à 72 octets et sa confirmation doit être identique."
		status = 422
	case err != nil:
		http.Error(w, "Réinitialisation temporairement indisponible.", 503)
		return
	}
	h.render(w, r, v, status)
}

func (h *PasswordResetHandler) administrative(w http.ResponseWriter, r *http.Request) {
	person, err := adminID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.s.RequestForPerson(r.Context(), person)
	result := "sent"
	switch {
	case errors.Is(err, authorization.ErrForbidden):
		http.Error(w, "Accès refusé", 403)
		return
	case errors.Is(err, passwordreset.ErrNoChannel):
		result = "no_channel"
	case errors.Is(err, passwordreset.ErrDelivery):
		result = "send_failed"
	case errors.Is(err, passwordreset.ErrLimited):
		result = "limited"
	case errors.Is(err, passwordreset.ErrIneligible):
		result = "ineligible"
	case err != nil:
		result = "unavailable"
	}
	http.Redirect(w, r, fmt.Sprintf("/persons/%d?password_reset=%s#compte-personne", person, result), 303)
}
