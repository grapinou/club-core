package router

import "testing"

func TestClubRoute(t *testing.T) {
	testRoute(t, "/club", "Le Club")
}
