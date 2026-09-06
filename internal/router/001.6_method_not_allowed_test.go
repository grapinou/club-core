package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grapinou/club-manager/internal/config"
)

func TestMethodNotAllowed(t *testing.T) {

	cfg := config.Config{
		SiteName: "Club Manager",
	}

	queries := &FakeQueries{}
	mux := New(cfg, queries)

	request := httptest.NewRequest(
		http.MethodPost,
		"/contact",
		nil,
	)

	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf(
			"statut obtenu : %d, statut attendu : %d",
			response.Code,
			http.StatusMethodNotAllowed,
		)
	}
}
