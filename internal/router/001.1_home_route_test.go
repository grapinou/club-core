package router

import "testing"

func TestHomeRoute(t *testing.T) {
	testRoute(t, "/", "Accueil")
}
