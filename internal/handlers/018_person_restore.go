package handlers

import (
	"net/http"

	"github.com/grapinou/club-manager/internal/database"
)

func PostRestorePersonHandler(queries database.PersonQueries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		id, err := parseID(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id incorrect", http.StatusBadRequest)
			return
		}

		err = queries.RestorePerson(r.Context(), id)
		if err != nil {
			http.Error(
				w,
				"impossible de restaurer la personne",
				http.StatusInternalServerError,
			)
			return
		}

		http.Redirect(
			w,
			r,
			"/persons/archived",
			http.StatusSeeOther,
		)
	}
}
