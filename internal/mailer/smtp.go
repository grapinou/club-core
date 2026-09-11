package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type SMTPConfig struct {
	Host                     string
	Port                     int
	Username, Password, From string
	STARTTLS                 bool
}

func (c SMTPConfig) Validate() error {
	if strings.TrimSpace(c.Host) == "" || strings.ContainsAny(c.Host, "\r\n /") || c.Port < 1 || c.Port > 65535 {
		return errors.New("invalid SMTP host or port")
	}
	if !validAddress(c.From) {
		return errors.New("invalid SMTP_FROM")
	}
	if (c.Username == "") != (c.Password == "") {
		return errors.New("SMTP_USERNAME and SMTP_PASSWORD must be provided together")
	}
	if !c.STARTTLS {
		ip := net.ParseIP(c.Host)
		if c.Host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("SMTP_STARTTLS is required outside loopback")
		}
		if c.Username != "" {
			return errors.New("SMTP authentication requires STARTTLS")
		}
	}
	return nil
}
func validAddress(s string) bool {
	if strings.ContainsAny(s, "\r\n") {
		return false
	}
	_, err := mail.ParseAddress(s)
	return err == nil
}

type SMTP struct{ config SMTPConfig }

func NewSMTP(c SMTPConfig) (*SMTP, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &SMTP{config: c}, nil
}

// Send uses a bounded, cancellable connection and requires advertised STARTTLS
// when configured. Errors expose only the failed stage, never server-supplied text.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	if !validAddress(m.From) || !validAddress(m.To) || strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("invalid email headers")
	}
	if m.From != s.config.From {
		return errors.New("email sender differs from SMTP_FROM")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port)))
	if err != nil {
		return errors.New("SMTP connection failed")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return errors.New("SMTP deadline failed")
	}
	client, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		return errors.New("SMTP greeting failed")
	}
	defer client.Close()
	if s.config.STARTTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP STARTTLS unavailable")
		}
		if err = client.StartTLS(&tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return errors.New("SMTP TLS failed")
		}
	}
	if s.config.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)); err != nil {
			return errors.New("SMTP authentication failed")
		}
	}
	from, _ := mail.ParseAddress(m.From)
	to, _ := mail.ParseAddress(m.To)
	if err = client.Mail(from.Address); err != nil {
		return errors.New("SMTP sender rejected")
	}
	if err = client.Rcpt(to.Address); err != nil {
		return errors.New("SMTP recipient rejected")
	}
	w, err := client.Data()
	if err != nil {
		return errors.New("SMTP DATA rejected")
	}
	header := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", from.String(), to.String(), mime.QEncoding.Encode("UTF-8", m.Subject), time.Now().Format(time.RFC1123Z))
	if _, err = w.Write([]byte(header)); err != nil {
		return errors.New("SMTP headers failed")
	}
	body := quotedprintable.NewWriter(w)
	if _, err = body.Write([]byte(m.Text)); err != nil {
		return errors.New("SMTP body failed")
	}
	if err = body.Close(); err != nil {
		return errors.New("SMTP encoding failed")
	}
	if err = w.Close(); err != nil {
		return errors.New("SMTP message not accepted")
	}
	// DATA acceptance is delivery to the relay; QUIT failure does not undo it.
	_ = client.Quit()
	return nil
}
