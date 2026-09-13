package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"

	"github.com/grapinou/club-core/internal/personalspace"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

func personalError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, personalspace.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	http.Error(w, "Votre espace est temporairement indisponible.", http.StatusServiceUnavailable)
}
func personalID(r *http.Request, key string) (int32, error) {
	id, err := strconv.ParseInt(r.PathValue(key), 10, 32)
	if err != nil || id <= 0 {
		return 0, personalspace.ErrNotFound
	}
	return int32(id), nil
}
func RegisterPersonalSpace(mux *http.ServeMux, site string, s *personalspace.Service, csrf *websecurity.CSRF) {
	register := func(pattern string, load func(*http.Request) (views.PersonalPage, error)) {
		mux.Handle(pattern, RequireAuthenticated(csrf.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			v, err := load(r)
			if err != nil {
				personalError(w, r, err)
				return
			}
			v.SecurityData = pageSecurity(r)
			v.SiteName = site
			v.Title = "Mon espace - " + site
			var body bytes.Buffer
			if err = views.RenderPersonal(&body, v); err != nil {
				personalError(w, r, err)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(body.Bytes())
		}))))
	}
	mux.Handle("GET /me", RequireAuthenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})))
	register("GET /me/account", func(r *http.Request) (views.PersonalPage, error) {
		a, err := s.GetMyAccount(r.Context())
		return views.PersonalPage{Account: &views.PersonalAccountView{Account: a}}, err
	})
	register("GET /me/memberships/{id}", func(r *http.Request) (views.PersonalPage, error) {
		id, err := personalID(r, "id")
		if err != nil {
			return views.PersonalPage{}, err
		}
		m, err := s.GetMyMembership(r.Context(), id)
		return views.PersonalPage{Membership: &views.PersonalMembershipView{Membership: m}}, err
	})
	register("GET /me/children/{childID}", func(r *http.Request) (views.PersonalPage, error) {
		id, err := personalID(r, "childID")
		if err != nil {
			return views.PersonalPage{}, err
		}
		c, err := s.GetManagedChild(r.Context(), id)
		return views.PersonalPage{Child: &views.ChildOverviewView{Child: c}}, err
	})
	register("GET /me/children/{childID}/memberships/{membershipID}", func(r *http.Request) (views.PersonalPage, error) {
		child, err := personalID(r, "childID")
		if err != nil {
			return views.PersonalPage{}, err
		}
		id, err := personalID(r, "membershipID")
		if err != nil {
			return views.PersonalPage{}, err
		}
		m, err := s.GetManagedChildMembership(r.Context(), child, id)
		return views.PersonalPage{Membership: &views.PersonalMembershipView{Membership: m, ChildID: child}}, err
	})
}
