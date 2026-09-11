package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/mailer"
)

const DefaultRegistrationVerificationTTL = time.Hour

type Runtime struct {
	RegistrationVerificationTTL time.Duration
	BaseURL                     string
	EmailTransport              string
	SMTP                        mailer.SMTPConfig
	ActivationValidity          time.Duration
	Location                    *time.Location
	SecureCookies               bool
}

func LoadRuntime() (Runtime, error) { return loadRuntime(os.Getenv) }
func loadRuntime(env func(string) string) (Runtime, error) {
	c := Runtime{BaseURL: env("APP_BASE_URL"), EmailTransport: env("EMAIL_TRANSPORT")}
	if c.BaseURL == "" {
		c.BaseURL = "http://localhost:8080"
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") {
		return c, errors.New("APP_BASE_URL must be an HTTP(S) origin without credentials, query or path")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return c, errors.New("invalid APP_BASE_URL port")
		}
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return c, errors.New("APP_BASE_URL requires HTTPS outside loopback")
		}
	}
	c.BaseURL = strings.TrimSuffix(c.BaseURL, "/")
	c.SecureCookies = u.Scheme == "https"
	c.ActivationValidity = 24 * time.Hour
	if raw := env("ACTIVATION_CODE_TTL"); raw != "" {
		c.ActivationValidity, err = time.ParseDuration(raw)
		if err != nil || c.ActivationValidity <= 0 {
			return c, errors.New("invalid ACTIVATION_CODE_TTL")
		}
	}
	c.RegistrationVerificationTTL = DefaultRegistrationVerificationTTL
	if raw := env("REGISTRATION_VERIFICATION_TTL"); raw != "" {
		c.RegistrationVerificationTTL, err = time.ParseDuration(raw)
		if err != nil || c.RegistrationVerificationTTL <= 0 {
			return c, errors.New("invalid REGISTRATION_VERIFICATION_TTL")
		}
	}
	timezone := env("APP_TIMEZONE")
	if timezone == "" {
		timezone = "Europe/Paris"
	}
	c.Location, err = time.LoadLocation(timezone)
	if err != nil {
		return c, errors.New("invalid APP_TIMEZONE")
	}
	if c.EmailTransport == "" {
		c.EmailTransport = "disabled"
	}
	if c.EmailTransport != "disabled" && c.EmailTransport != "smtp" {
		return c, errors.New("EMAIL_TRANSPORT must be disabled or smtp")
	}
	if c.EmailTransport == "disabled" {
		return c, nil
	}
	c.SMTP = mailer.SMTPConfig{Host: env("SMTP_HOST"), Username: env("SMTP_USERNAME"), Password: env("SMTP_PASSWORD"), From: env("SMTP_FROM"), STARTTLS: true}
	c.SMTP.Port, err = strconv.Atoi(env("SMTP_PORT"))
	if err != nil {
		return c, errors.New("invalid SMTP_PORT")
	}
	if raw := env("SMTP_STARTTLS"); raw != "" {
		c.SMTP.STARTTLS, err = strconv.ParseBool(raw)
		if err != nil {
			return c, errors.New("invalid SMTP_STARTTLS")
		}
	}
	if err = c.SMTP.Validate(); err != nil {
		return c, err
	}
	return c, nil
}
