package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostRestorePersonHandler(t *testing.T) {

	request := httptest.NewRequest(
		http.MethodPost,
		"/persons/42/restore",
		nil,
	)

	request.SetPathValue("id", "42")

	response := httptest.NewRecorder()

	queries := &recordingPersonQueries{}

	PostRestorePersonHandler(queries)(
		response,
		request,
	)

	if !queries.RestorePersonCalled {
		t.Error("RestorePerson aurait dû être appelée")
	}

	if queries.RestorePersonReceived != 42 {
		t.Errorf(
			"id obtenu : %d, attendu : %d",
			queries.RestorePersonReceived,
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

	if location != "/persons/archived" {
		t.Errorf(
			"redirection obtenue : %q, attendue : %q",
			location,
			"/persons/archived",
		)
	}
}
