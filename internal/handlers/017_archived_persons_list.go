package handlers

import (
	"net/http"

	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database"
	"github.com/grapinou/club-core/internal/views"
)

func ArchivedPersonsListHandler(cfg config.Config, queries database.PersonQueries) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		persons, err := queries.ListArchivedPersons(r.Context())
		if err != nil {
			http.Error(
				w,
				"impossible de récupérer les personnes archivées",
				http.StatusInternalServerError,
			)
			return
		}

		var personData []views.PersonData

		for _, person := range persons {

			email := "Non renseigné"
			phoneNumber := "Non renseigné"
			address := "Non renseigné"

			if person.Email.Valid {
				email = person.Email.String
			}

			if person.PhoneNumber.Valid {
				phoneNumber = person.PhoneNumber.String
			}

			if person.Address.Valid {
				address = person.Address.String
			}

			personData = append(personData, views.PersonData{
				ID:          person.ID,
				FirstName:   person.FirstName,
				LastName:    person.LastName,
				BirthDate:   person.BirthDate.Time.Format("02/01/2006"),
				Email:       email,
				PhoneNumber: phoneNumber,
				Address:     address,
				CreatedAt:   person.CreatedAt.Time.Format("02/01/2006 15:04"),
			})
		}

		data := views.PersonsData{SecurityData: pageSecurity(r),
			SiteName: cfg.SiteName,
			Title:    "Personnes archivées - " + cfg.SiteName,
			Persons:  personData,
		}

		if err := views.RenderArchivedPersonsList(w, data); err != nil {
			http.Error(
				w,
				"impossible d'afficher les personnes archivées",
				http.StatusInternalServerError,
			)
			return
		}
	}
}
