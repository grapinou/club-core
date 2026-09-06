package router

import "testing"

func TestContactRoute(t *testing.T) {
	testRoute(t, "/contact", "Contact")
}
