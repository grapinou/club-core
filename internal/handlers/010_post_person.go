package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

func PostPersonHandler(site string, queries database.PersonQueries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		firstName := strings.TrimSpace(r.FormValue("FirstName"))
		lastName := strings.TrimSpace(r.FormValue("LastName"))

		if firstName == "" || lastName == "" {
			personFormError(w, r, site, "Nom et prénom obligatoires")
			return
		}

		birthDate, err := frenchBirthDate(r.FormValue("Birthdate"), false)
		if err != nil {
			personFormError(w, r, site, "Date de naissance invalide : utilisez JJ/MM/AAAA.")
			return
		}

		datas := dbsqlc.CreatePersonParams{
			FirstName:   firstName,
			LastName:    lastName,
			BirthDate:   birthDate,
			PhoneNumber: pgTypeText(r.FormValue("PhoneNumber")),
			Email:       pgTypeText(r.FormValue("Email")),
			Address:     pgTypeText(r.FormValue("Address")),
		}

		person, err := queries.CreatePerson(r.Context(), datas)
		if err != nil {
			http.Error(w, "erreur lors de la création de la personne", http.StatusInternalServerError)
			return
		}

		destination := "/persons"
		if r.FormValue("after") == "trial" {
			destination = fmt.Sprintf("/persons/%d/trials/new", person.ID)
		}
		if r.FormValue("after") == "membership" {
			destination = fmt.Sprintf("/persons/%d/memberships/new", person.ID)
		}
		http.Redirect(w, r, destination, http.StatusSeeOther)
	}
}
