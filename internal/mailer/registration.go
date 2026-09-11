package mailer

import (
	"bytes"
	_ "embed"
	"text/template"
)

//go:embed templates/registration_verification.txt
var registrationText string
var registrationTemplate = template.Must(template.New("registration").Parse(registrationText))

// All parameters are transient. Do not log the resulting message.
func RegistrationMessage(from, to, verificationURL, reference, code string) (Message, error) {
	var body bytes.Buffer
	err := registrationTemplate.Execute(&body, struct{ URL, Reference, Code string }{verificationURL, reference, code})
	if err != nil {
		return Message{}, err
	}
	return Message{From: from, To: to, Subject: "Vérification de votre demande Club Core", Text: body.String()}, nil
}
