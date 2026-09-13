package views

// Status is a presentation mapping only; domain validation remains in services.
type Status struct{ Label, Class string }

func DisplayStatus(code string) Status {
	switch code {
	case "registered":
		return Status{"Programmé", "text-bg-info"}
	case "attended":
		return Status{"Présent", "text-bg-success"}
	case "no_show":
		return Status{"Absent", "text-bg-secondary"}
	case "pending":
		return Status{"En attente", "text-bg-warning"}
	case "active":
		return Status{"Active", "text-bg-success"}
	case "ended":
		return Status{"Terminée", "text-bg-secondary"}
	case "cancelled":
		return Status{"Annulée", "text-bg-secondary"}
	case "received":
		return Status{"Reçue", "text-bg-secondary"}
	case "resolved":
		return Status{"Résolue", "text-bg-success"}
	case "needs_review", "awaiting_review", "awaiting_identity_review":
		return Status{"À vérifier", "text-bg-warning"}
	case "awaiting_identity":
		return Status{"Identité à vérifier", "text-bg-warning"}
	case "awaiting_email_verification":
		return Status{"Vérification email en cours", "text-bg-info"}
	case "membership_created":
		return Status{"Demande d’adhésion enregistrée", "text-bg-success"}
	case "granted":
		return Status{"Accordé", "text-bg-success"}
	case "refused":
		return Status{"Refusé", "text-bg-secondary"}
	case "withdrawn":
		return Status{"Retiré", "text-bg-secondary"}
	default:
		return Status{"Non renseigné", "text-bg-secondary"}
	}
}
func (SecurityData) StatusLabel(code string) string     { return DisplayStatus(code).Label }
func (SecurityData) StatusClassFor(code string) string  { return DisplayStatus(code).Class }
func (SecurityData) RelationLabel(code string) string   { return relationship(code) }
func (SecurityData) ConfidenceLabel(code string) string { return confidence(code) }
func (SecurityData) ResolutionLabel(code string) string {
	if code == "new_person" {
		return "Nouvelle fiche"
	}
	if code == "existing_person" {
		return "Fiche existante"
	}
	return "À résoudre"
}
