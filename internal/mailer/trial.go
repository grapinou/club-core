package mailer

import (
	"bytes"
	_ "embed"
	"text/template"
)

//go:embed templates/trial_confirmation.txt
var trialConfirmationText string
var trialConfirmationTemplate = template.Must(template.New("trial-confirmation").Parse(trialConfirmationText))

type TrialConfirmationData struct {
	Club, Registrant, Activity, Group, Practice, Date, Start, End, Location, Address string
	SessionDescription, ItemsToBring, EquipmentOffer, EquipmentDetails               string
	EquipmentNeeded                                                                  bool
}

func TrialConfirmationMessage(from, to string, data TrialConfirmationData) (Message, error) {
	var body bytes.Buffer
	if err := trialConfirmationTemplate.Execute(&body, data); err != nil {
		return Message{}, err
	}
	return Message{From: from, To: to, Subject: "Confirmation de votre essai · " + data.Club, Text: body.String()}, nil
}
