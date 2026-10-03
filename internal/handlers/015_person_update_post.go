package handlers

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/views"
)

func PostUpdatePersonHandler(site string, queries database.PersonQueries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		id, err := parseID(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id incorrect", http.StatusBadRequest)
			return
		}

		invalid := func(message string) {
			data := views.PersonUpdateFormPageData{SecurityData: pageSecurity(r), SiteName: site, Title: "Modifier une personne - " + site, Error: message, Person: views.PersonUpdateFormData{ID: id, FirstName: r.FormValue("FirstName"), LastName: r.FormValue("LastName"), BirthDate: r.FormValue("Birthdate"), PhoneNumber: r.FormValue("PhoneNumber"), Email: r.FormValue("Email"), Address: r.FormValue("Address")}}
			var body bytes.Buffer
			if err := views.RenderPersonUpdateForm(&body, data); err != nil {
				http.Error(w, "Erreur interne du serveur", 500)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(body.Bytes())
		}

		firstName := strings.TrimSpace(r.FormValue("FirstName"))
		lastName := strings.TrimSpace(r.FormValue("LastName"))

		if firstName == "" || lastName == "" {
			invalid("Nom et prénom obligatoires")
			return
		}

		birthDate, err := frenchBirthDate(r.FormValue("Birthdate"), false)
		if err != nil {
			invalid("Date de naissance invalide : utilisez JJ/MM/AAAA.")
			return
		}

		datas := dbsqlc.UpdatePersonParams{
			ID:          id,
			FirstName:   firstName,
			LastName:    lastName,
			BirthDate:   birthDate,
			PhoneNumber: pgTypeText(r.FormValue("PhoneNumber")),
			Email:       pgTypeText(r.FormValue("Email")),
			Address:     pgTypeText(r.FormValue("Address")),
		}

		_, err = queries.UpdatePerson(r.Context(), datas)
		if err != nil {
			http.Error(w, "erreur lors de la modification de la personne", http.StatusInternalServerError)
			return

		}

		http.Redirect(
			w,
			r,
			"/persons",
			http.StatusSeeOther,
		)

	}
}
