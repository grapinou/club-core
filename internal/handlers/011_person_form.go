package handlers

import (
	"bytes"
	"net/http"

	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/views"
)

func PersonFormHandler(cfg config.Config) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		data := personFormData(cfg.SiteName, r)

		err := views.RenderPersonForm(w, data)

		if err != nil {
			http.Error(
				w,
				"Erreur interne du serveur",
				http.StatusInternalServerError,
			)
			return
		}
	}

}

func personFormData(site string, r *http.Request) views.PersonFormData {
	data := views.PersonFormData{SecurityData: pageSecurity(r), SiteName: site, Title: "Ajouter une personne - " + site, AfterTrial: r.FormValue("after") == "trial", AfterMembership: r.FormValue("after") == "membership", Values: r.PostForm}
	if data.AfterTrial {
		data.CurrentPath = "/trials/new"
	}
	if data.AfterMembership {
		data.CurrentPath = "/memberships/new"
	}
	return data
}

func personFormError(w http.ResponseWriter, r *http.Request, site, message string) {
	data := personFormData(site, r)
	data.Error = message
	var body bytes.Buffer
	if err := views.RenderPersonForm(&body, data); err != nil {
		http.Error(w, "Erreur interne du serveur", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(body.Bytes())
}
