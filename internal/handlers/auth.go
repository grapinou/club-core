package handlers

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

type Activator interface {
	Activate(context.Context, string, string, string) (dbsqlc.User, error)
}
type AuthHandler struct {
	siteName   string
	activation Activator
	auth       *auth.Service
	sessions   *auth.Sessions
	limiter    *AttemptLimiter
	csrf       *websecurity.CSRF
}

func NewAuthHandler(siteName string, a Activator, login *auth.Service, sessions *auth.Sessions, secure bool) *AuthHandler {
	return &AuthHandler{siteName: siteName, activation: a, auth: login, sessions: sessions, limiter: NewAttemptLimiter(), csrf: websecurity.NewCSRF(secure)}
}
func (h *AuthHandler) Register(mux *http.ServeMux) {
	mux.Handle("GET /activate", h.sensitive(h.getActivate))
	mux.Handle("POST /activate", h.sensitive(h.postActivate))
	mux.Handle("GET /login", h.sensitive(h.getLogin))
	mux.Handle("POST /login", h.sensitive(h.postLogin))
	mux.Handle("POST /logout", RequireAuthenticated(h.sensitive(h.postLogout)))
}
func (h *AuthHandler) render(w http.ResponseWriter, r *http.Request, activation bool, message string) {
	_, loggedIn := auth.UserID(r.Context())
	title := "Connexion"
	if activation {
		title = "Activer votre compte"
	}
	data := views.AuthData{SecurityData: pageSecurity(r), SiteName: h.siteName, Title: title + " - " + h.siteName, Activation: activation, Message: message, LoggedIn: loggedIn}
	var buf bytes.Buffer
	if err := views.RenderAuth(&buf, data); err != nil {
		http.Error(w, "Erreur interne du serveur", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}
func (h *AuthHandler) getActivate(w http.ResponseWriter, r *http.Request) {
	message := ""
	if r.URL.Query().Get("error") == "1" {
		message = "Impossible d'activer le compte avec ces informations."
	}
	h.render(w, r, true, message)
}
func (h *AuthHandler) postActivate(w http.ResponseWriter, r *http.Request) {
	password := r.PostForm.Get("password")
	if password != r.PostForm.Get("confirmation") {
		http.Redirect(w, r, "/activate?error=1", http.StatusSeeOther)
		return
	}
	_, err := h.activation.Activate(r.Context(), strings.TrimSpace(r.PostForm.Get("username")), strings.TrimSpace(r.PostForm.Get("code")), password)
	if err != nil {
		http.Redirect(w, r, "/activate?error=1", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login?activated=1", http.StatusSeeOther)
}
func (h *AuthHandler) getLogin(w http.ResponseWriter, r *http.Request) {
	message := ""
	if r.URL.Query().Get("activated") == "1" {
		message = "Votre compte est activé. Vous pouvez maintenant vous connecter."
	}
	if r.URL.Query().Get("error") == "1" {
		message = "Identifiant ou mot de passe incorrect."
	}
	h.render(w, r, false, message)
}
func (h *AuthHandler) postLogin(w http.ResponseWriter, r *http.Request) {
	id, err := h.auth.Authenticate(r.Context(), strings.TrimSpace(r.PostForm.Get("username")), r.PostForm.Get("password"))
	if err != nil {
		http.Redirect(w, r, "/login?error=1", http.StatusSeeOther)
		return
	}
	if err = h.sessions.Create(w, r, id); err != nil {
		http.Error(w, "Connexion temporairement indisponible.", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (h *AuthHandler) postLogout(w http.ResponseWriter, r *http.Request) {
	h.sessions.Delete(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
