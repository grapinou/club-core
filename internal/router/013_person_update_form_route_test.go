package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/config"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestUpdatePersonFormRoute(t *testing.T) {
	cfg := config.Config{
		SiteName: "Club Manager",
	}

	queries := &FakeQueries{
		PersonByID: dbsqlc.Person{
			ID:        42,
			FirstName: "Robin",
			LastName:  "Des Bois",

			BirthDate: pgtype.Date{
				Time:  time.Date(1990, 5, 12, 0, 0, 0, 0, time.UTC),
				Valid: true,
			},
		},
	}

	mux := newTestRouter(cfg, queries)

	request := httptest.NewRequest(
		http.MethodGet,
		"/persons/42/edit",
		nil,
	)

	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Errorf(
			"statut obtenu : %d, statut attendu : %d",
			response.Code,
			http.StatusSeeOther,
		)
	}

	if queries.GetPersonByIDReceived != 0 {
		t.Errorf(
			"id reçu : %d, id attendu : %d",
			queries.GetPersonByIDReceived,
			0,
		)
	}
}
