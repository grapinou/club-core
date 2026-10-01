package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/emergencycontacts"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
)

func RegisterEmergencyContacts(mux *http.ServeMux, site string, s *emergencycontacts.Service, access *Access, csrf *websecurity.CSRF) {
	for _, path := range []string{"/persons/{id}/emergency", "/me/emergency", "/me/children/{childID}/emergency"} {
		office := path == "/persons/{id}/emergency"
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			scope := emergencycontacts.Scope{Office: office}
			back := "/me/account"
			var err error
			if office {
				scope.Child, err = adminID(r, "id")
				back = fmt.Sprintf("/persons/%d", scope.Child)
			} else if r.PathValue("childID") != "" {
				scope.Child, err = personalID(r, "childID")
				back = fmt.Sprintf("/me/children/%d", scope.Child)
			}
			v := views.EmergencyView{SecurityData: pageSecurity(r), SiteName: site, Title: "Contacts d’urgence - " + site, Path: r.URL.Path, Back: back, Form: r.PostForm}
			status := 200
			if err == nil && r.Method == "POST" {
				id, _ := strconv.ParseInt(r.PostForm.Get("contact_id"), 10, 32)
				rank, _ := strconv.ParseInt(r.PostForm.Get("priority"), 10, 32)
				v.FailedID = int32(id)
				in := emergencycontacts.Input{FirstName: r.PostForm.Get("first_name"), LastName: r.PostForm.Get("last_name"), Phone: r.PostForm.Get("phone_number"), Email: r.PostForm.Get("email"), Relationship: r.PostForm.Get("relationship"), Priority: int32(rank)}
				err = s.Mutate(r.Context(), scope, r.PostForm.Get("action"), int32(id), in)
				if err == nil {
					http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
					return
				}
				var fields emergencycontacts.Fields
				if errors.As(err, &fields) {
					status = 422
					v.Error = "Vérifiez le prénom, le nom, le téléphone, le lien et la priorité du contact."
					err = nil
				}
			}
			if err == nil {
				v.Page, err = s.Page(r.Context(), scope)
			}
			if err != nil {
				if errors.Is(err, emergencycontacts.ErrUnavailable) {
					http.NotFound(w, r)
				} else if errors.Is(err, authorization.ErrForbidden) {
					http.Error(w, "Accès refusé", 403)
				} else {
					http.Error(w, "Les contacts sont temporairement indisponibles.", 503)
				}
				return
			}
			var b bytes.Buffer
			if err = views.RenderEmergency(&b, v); err != nil {
				http.Error(w, "Page temporairement indisponible.", 503)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(status)
			_, _ = w.Write(b.Bytes())
		})
		for _, method := range []string{"GET", "POST"} {
			var h http.Handler = csrf.Protect(handler)
			if office {
				h = access.RequirePermission(authorization.PersonsWrite, h)
			} else {
				h = RequireAuthenticated(h)
			}
			mux.Handle(method+" "+path, h)
		}
	}
}
