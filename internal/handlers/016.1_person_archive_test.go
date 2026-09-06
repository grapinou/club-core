package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostArchivePersonHandler(t *testing.T) {

	request := httptest.NewRequest(
		http.MethodPost,
		"/persons/42/archive",
		nil,
	)

	request.SetPathValue("id", "42")

	response := httptest.NewRecorder()

	queries := &recordingPersonQueries{}

	PostArchivePersonHandler(queries)(
		response,
		request,
	)

	if !queries.ArchivePersonCalled {
		t.Error("ArchivePerson aurait dû être appelée")
	}

	if queries.ArchivePersonReceived != 42 {
		t.Errorf(
			"id obtenu : %d, attendu : %d",
			queries.ArchivePersonReceived,
			42,
		)
	}

	if response.Code != http.StatusSeeOther {
		t.Errorf(
			"statut obtenu : %d, statut attendu : %d",
			response.Code,
			http.StatusSeeOther,
		)
	}

	location := response.Header().Get("Location")

	if location != "/persons" {
		t.Errorf(
			"redirection obtenue : %q, attendue : %q",
			location,
			"/persons",
		)
	}
}
