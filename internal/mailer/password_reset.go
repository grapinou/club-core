package mailer

import (
	"bytes"
	_ "embed"
	"fmt"
	"math"
	"text/template"
	"time"
)

//go:embed templates/password_reset.txt
var passwordResetText string
var passwordResetTemplate = template.Must(template.New("password-reset").Parse(passwordResetText))

func PasswordResetMessage(from, to, username, link string, ttl time.Duration) (Message, error) {
	minutes := int(math.Ceil(ttl.Minutes()))
	duration := fmt.Sprintf("%d minutes", minutes)
	if minutes == 1 {
		duration = "1 minute"
	}
	var body bytes.Buffer
	err := passwordResetTemplate.Execute(&body, struct{ Username, Link, Duration string }{username, link, duration})
	return Message{From: from, To: to, Subject: "Réinitialisation de votre mot de passe Club Core", Text: body.String()}, err
}

func PasswordChangedMessage(from, to string) Message {
	return Message{From: from, To: to, Subject: "Votre mot de passe Club Core a été modifié", Text: "Le mot de passe de votre compte Club Core vient d’être modifié.\nSi vous n’êtes pas à l’origine de cette opération, contactez le club.\n"}
}
