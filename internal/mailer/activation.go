package mailer

import (
	"bytes"
	_ "embed"
	"text/template"

	"github.com/grapinou/club-core/internal/activation"
)

//go:embed templates/activation.txt
var activationText string
var activationTemplate = template.Must(template.New("activation").Parse(activationText))

func ActivationMessage(from, activationURL string, d activation.Delivery) (Message, error) {
	var body bytes.Buffer
	err := activationTemplate.Execute(&body, struct{ Username, Code, ActivationURL string }{d.Username, d.PlaintextCode, activationURL})
	if err != nil {
		return Message{}, err
	}
	to := ""
	if d.RecipientEmail != nil {
		to = *d.RecipientEmail
	}
	return Message{From: from, To: to, Subject: "Activation de votre compte Club Core", Text: body.String()}, nil
}
