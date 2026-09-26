package handlers

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/grapinou/club-core/internal/initialsetup"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

type SetupHandler struct {
	site    string
	service *initialsetup.Service
	limiter *AttemptLimiter
}

func NewSetupHandler(site string, service *initialsetup.Service) *SetupHandler {
	return &SetupHandler{site: site, service: service, limiter: NewAttemptLimiter()}
}

func (h *SetupHandler) Register(mux *http.ServeMux, csrf *websecurity.CSRF) {
	mux.Handle("GET /setup", csrf.Protect(http.HandlerFunc(h.get)))
	protected := csrf.Protect(http.HandlerFunc(h.post))
	mux.Handle("POST /setup", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.limiter.AllowRequest(r) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "900")
			http.Error(w, "Trop de tentatives. Réessayez plus tard.", http.StatusTooManyRequests)
			return
		}
		protected.ServeHTTP(w, r)
	}))
}

func (h *SetupHandler) render(w http.ResponseWriter, r *http.Request, v views.SetupView, status int) {
	v.SecurityData = pageSecurity(r)
	v.SiteName = h.site
	v.Title = "Configuration initiale - " + h.site
	var body bytes.Buffer
	if err := views.RenderSetup(&body, v); err != nil {
		http.Error(w, "Page temporairement indisponible", 503)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func (h *SetupHandler) get(w http.ResponseWriter, r *http.Request) {
	state, err := h.service.Status(r.Context())
	if err != nil {
		http.Error(w, "Configuration temporairement indisponible", 503)
		return
	}
	if state.Initialized {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	mode := "form"
	if !state.Ready {
		mode = "unavailable"
	}
	h.render(w, r, views.SetupView{Mode: mode}, 200)
}

func (h *SetupHandler) post(w http.ResponseWriter, r *http.Request) {
	in := initialsetup.Input{Secret: r.PostForm.Get("setup_secret"), FirstName: r.PostForm.Get("first_name"), LastName: r.PostForm.Get("last_name"), Username: r.PostForm.Get("username"), Email: r.PostForm.Get("email"), Password: r.PostForm.Get("password"), Confirmation: r.PostForm.Get("confirmation")}
	_, err := h.service.Complete(r.Context(), in)
	if err == nil {
		http.Redirect(w, r, "/login?setup=1", http.StatusSeeOther)
		return
	}
	if errors.Is(err, initialsetup.ErrInitialized) {
		h.render(w, r, views.SetupView{Mode: "configured"}, 409)
		return
	}
	if errors.Is(err, initialsetup.ErrSecretUnavailable) {
		h.render(w, r, views.SetupView{Mode: "unavailable"}, 409)
		return
	}
	if errors.Is(err, initialsetup.ErrInvalidInput) || errors.Is(err, initialsetup.ErrInvalidSecret) {
		message := "Vérifiez les informations du compte et réessayez."
		if errors.Is(err, initialsetup.ErrInvalidSecret) {
			message = "Le code de configuration est incorrect ou n’est plus valide."
		}
		h.render(w, r, views.SetupView{Mode: "form", Error: message, FirstName: in.FirstName, LastName: in.LastName, Username: in.Username, Email: in.Email}, 422)
		return
	}
	http.Error(w, "Configuration temporairement indisponible", 503)
}
