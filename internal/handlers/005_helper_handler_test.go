package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testHandler vérifie le comportement commun des handlers HTTP simples.
//
// Le helper :
//   - simule une requête GET vers la route indiquée ;
//   - exécute directement le handler reçu en paramètre ;
//   - vérifie que le statut HTTP obtenu est 200 OK ;
//   - vérifie que la réponse contient le texte attendu.
//
// Attention : ce helper teste directement le handler.
// Il ne vérifie pas que la route est correctement enregistrée dans le routeur.
func testHandler(
	t *testing.T,
	route string,
	expectedContent string,
	handlerToTest http.HandlerFunc,
) {
	// Indique à Go que cette fonction est un outil utilisé par les tests.
	// En cas d'échec, le message pointera vers le test appelant ce helper.
	t.Helper()

	// Crée une requête HTTP GET simulée.
	// Aucun serveur et aucune connexion réseau ne sont nécessaires.
	request := httptest.NewRequest(http.MethodGet, route, nil)

	// Crée un faux ResponseWriter qui conserve en mémoire
	// le statut, les en-têtes et le contenu écrits par le handler.
	response := httptest.NewRecorder()

	// Exécute directement le handler avec la requête simulée
	// et l'enregistreur de réponse.
	handlerToTest(response, request)

	// Vérifie que le handler répond avec le statut HTTP 200 OK.
	if response.Code != http.StatusOK {
		t.Errorf(
			"statut obtenu : %d, statut attendu : %d",
			response.Code,
			http.StatusOK,
		)
	}

	// Récupère sous forme de chaîne le contenu écrit dans la réponse.
	body := response.Body.String()

	// Vérifie que la réponse contient le texte caractéristique
	// de la page testée.
	if !strings.Contains(body, expectedContent) {
		t.Errorf(
			"la page ne contient pas le texte %q ; contenu obtenu : %q",
			expectedContent,
			body,
		)
	}
}
