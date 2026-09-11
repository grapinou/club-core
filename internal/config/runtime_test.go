package config

import "testing"

func TestRuntime(t *testing.T) {
	base := map[string]string{
		"APP_BASE_URL": "https://club.example.test", "EMAIL_TRANSPORT": "smtp",
		"SMTP_HOST": "smtp.example.test", "SMTP_PORT": "587", "SMTP_FROM": "Club <club@example.test>",
		"SMTP_USERNAME": "user", "SMTP_PASSWORD": "test-only-secret",
	}
	load := func(values map[string]string) (Runtime, error) {
		return loadRuntime(func(key string) string { return values[key] })
	}
	cfg, err := load(base)
	if err != nil || !cfg.SMTP.STARTTLS || !cfg.SecureCookies || cfg.BaseURL != "https://club.example.test" {
		t.Fatal("valid config rejected")
	}
	for _, tc := range []struct{ key, value string }{
		{"APP_BASE_URL", "https://user:secret@club.example.test"}, {"APP_BASE_URL", "http://club.example.test"},
		{"APP_BASE_URL", "https://club.example.test/path"},
		{"APP_BASE_URL", "https://club.example.test?"},
		{"APP_BASE_URL", "https://:8080"},
		{"APP_BASE_URL", "https://club.example.test:99999"}, {"SMTP_PORT", "0"}, {"SMTP_PORT", "invalid"},
		{"SMTP_FROM", "bad\r\nheader"}, {"SMTP_HOST", ""}, {"SMTP_USERNAME", ""},
		{"SMTP_STARTTLS", "false"}, {"SMTP_STARTTLS", "invalid"}, {"EMAIL_TRANSPORT", "log"},
		{"REGISTRATION_VERIFICATION_TTL", "0s"}, {"REGISTRATION_VERIFICATION_TTL", "-1h"}, {"REGISTRATION_VERIFICATION_TTL", "bad"}, {"ACTIVATION_CODE_TTL", "0s"}, {"APP_TIMEZONE", "unknown"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			values[tc.key] = tc.value
			if _, err := load(values); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	cfg, err = load(map[string]string{})
	if err != nil || cfg.EmailTransport != "disabled" || cfg.SecureCookies || cfg.RegistrationVerificationTTL != DefaultRegistrationVerificationTTL {
		t.Fatal("development defaults")
	}
	cfg, err = load(map[string]string{"EMAIL_TRANSPORT": "smtp", "SMTP_HOST": "127.0.0.1", "SMTP_PORT": "1025", "SMTP_FROM": "club@example.test", "SMTP_STARTTLS": "false"})
	if err != nil || cfg.SMTP.STARTTLS {
		t.Fatal("local relay", err)
	}
}
