package handlers

import (
	"net/http"
	"strings"

	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

func PostUpdatePersonHandler(queries database.PersonQueries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		id, err := parseID(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id incorrect", http.StatusBadRequest)
			return
		}

		firstName := strings.TrimSpace(r.FormValue("FirstName"))
		lastName := strings.TrimSpace(r.FormValue("LastName"))

		if firstName == "" || lastName == "" {
			http.Error(w, "Nom et prénom obligatoires", http.StatusBadRequest)
			return
		}

		birthDate, err := pgTypeDate(r.FormValue("Birthdate"))
		if err != nil {
			http.Error(w, "Date de naissance invalide", http.StatusBadRequest)
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
