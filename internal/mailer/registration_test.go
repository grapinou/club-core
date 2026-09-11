package mailer

import (
	"strings"
	"testing"
)

func TestRegistrationMessage(t *testing.T) {
	m, err := RegistrationMessage("club@example.test", "recipient@example.test", "https://club.example.test/registration/verify", "opaque-reference", "12345678901234567890")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"opaque-reference", "12345678901234567890", "https://club.example.test/registration/verify"} {
		if !strings.Contains(m.Text, v) {
			t.Fatal("missing mail value")
		}
	}
	for _, v := range []string{"password", "hash", "Membership", "User", "guardian", "recipient@example.test", "SMTP"} {
		if strings.Contains(m.Text, v) {
			t.Fatal("unnecessary data in mail")
		}
	}
}
