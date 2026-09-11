package mailer

import (
	"bufio"
	"context"
	"io"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/activation"
)

func TestActivationMessage(t *testing.T) {
	recipient := "member@example.test"
	message, err := ActivationMessage("club@example.test", "https://club.example.test/activate", activation.Delivery{Username: "remi.dupont", PlaintextCode: "12345678901234567890", RecipientEmail: &recipient})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"remi.dupont", "12345678901234567890", "https://club.example.test/activate", "Club Core"} {
		if !strings.Contains(message.Text, text) {
			t.Fatal("missing content", text)
		}
	}
	for _, text := range []string{"password_hash", "$2a$", "?code=", "SMTP_PASSWORD"} {
		if strings.Contains(message.Text, text) {
			t.Fatal("unexpected secret", text)
		}
	}
	if message.To != recipient {
		t.Fatal("recipient")
	}
}

// A real loopback SMTP conversation, with no Internet or relay dependency.
func TestSMTPRelay(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rejected"}[reject], func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			received := make(chan string, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, e := listener.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(conn)
				_, _ = io.WriteString(conn, "220 local test relay\r\n")
				for {
					line, e := reader.ReadString('\n')
					if e != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						_, _ = io.WriteString(conn, "250 localhost\r\n")
					case strings.HasPrefix(line, "MAIL"):
						_, _ = io.WriteString(conn, "250 sender ok\r\n")
					case strings.HasPrefix(line, "RCPT"):
						if reject {
							_, _ = io.WriteString(conn, "550 secret-server-reply\r\n")
						} else {
							_, _ = io.WriteString(conn, "250 recipient ok\r\n")
						}
					case strings.HasPrefix(line, "DATA"):
						_, _ = io.WriteString(conn, "354 send message\r\n")
						var body strings.Builder
						for {
							line, e = reader.ReadString('\n')
							if e != nil {
								return
							}
							if line == ".\r\n" {
								break
							}
							body.WriteString(line)
						}
						received <- body.String()
						_, _ = io.WriteString(conn, "250 accepted\r\n")
					case strings.HasPrefix(line, "QUIT"):
						_, _ = io.WriteString(conn, "221 goodbye\r\n")
						return
					default:
						_, _ = io.WriteString(conn, "500 unexpected\r\n")
					}
				}
			}()
			sender, err := NewSMTP(SMTPConfig{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, From: "club@example.test"})
			if err != nil {
				t.Fatal(err)
			}
			err = sender.Send(t.Context(), Message{From: "club@example.test", To: "member@example.test", Subject: "Activation été", Text: "Identifiant : remi.dupont\nCode : 123456\nhttps://example.test/activate"})
			if reject {
				if err == nil || strings.Contains(err.Error(), "secret-server-reply") {
					t.Fatal("unsafe error")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				parsed, e := mail.ReadMessage(strings.NewReader(<-received))
				if e != nil {
					t.Fatal(e)
				}
				body, e := io.ReadAll(quotedprintable.NewReader(parsed.Body))
				if e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(string(body), "Code : 123456") {
					t.Fatal("bad SMTP content")
				}
			}
			<-done
		})
	}
}
func TestSMTPValidationAndCancellation(t *testing.T) {
	if _, err := NewSMTP(SMTPConfig{Host: "smtp.example.test", Port: 587, From: "club@example.test"}); err == nil {
		t.Fatal("plaintext remote relay allowed")
	}
	sender, err := NewSMTP(SMTPConfig{Host: "127.0.0.1", Port: 1, From: "club@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = sender.Send(t.Context(), Message{From: "club@example.test", To: "victim@example.test\r\nBcc: other@example.test"}); err == nil {
		t.Fatal("header injection")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = sender.Send(ctx, Message{From: "club@example.test", To: "member@example.test"}); err == nil {
		t.Fatal("cancellation ignored")
	}
	if err = (Disabled{}).Send(t.Context(), Message{}); err != ErrDisabled {
		t.Fatal("disabled transport pretends delivery")
	}
}

func TestSMTPRequiresSTARTTLSAndHonorsInFlightCancellation(t *testing.T) {
	for _, stall := range []bool{false, true} {
		t.Run(map[bool]string{false: "no STARTTLS", true: "cancel greeting"}[stall], func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			accepted := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, e := listener.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				close(accepted)
				if stall {
					_, _ = io.Copy(io.Discard, conn)
					return
				}
				_, _ = io.WriteString(conn, "220 relay\r\n")
				reader := bufio.NewReader(conn)
				if _, e = reader.ReadString('\n'); e != nil {
					return
				}
				_, _ = io.WriteString(conn, "250 relay without TLS\r\n")
				_, _ = io.Copy(io.Discard, conn)
			}()
			sender, err := NewSMTP(SMTPConfig{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, STARTTLS: true, From: "club@example.test"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if stall {
				go func() { <-accepted; cancel() }()
			}
			err = sender.Send(ctx, Message{From: "club@example.test", To: "member@example.test"})
			if err == nil {
				t.Fatal("unsafe SMTP success")
			}
			if !stall && err.Error() != "SMTP STARTTLS unavailable" {
				t.Fatal(err)
			}
			<-done
		})
	}
}
