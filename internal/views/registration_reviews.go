package views

import (
	"embed"
	"html/template"
	"io"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/identityresolution"
)

type RegistrationListView struct {
	SecurityData
	SiteName, Title string
	Rows            []RegistrationRowView
}
type RegistrationRowView struct {
	ID                                             int32
	Name, BirthDate, CreatedAt, Status, Confidence string
	Count                                          int32
	Awaiting                                       bool
}
type RegistrationFieldView struct {
	Label, Declared, Existing string
	Different                 bool
}
type RegistrationCandidateView struct {
	ID                                    int32
	Name, Confidence, DetectedAt, Account string
	Archived                              bool
	Reasons                               []string
	Fields                                []RegistrationFieldView
}
type RegistrationApplicationView struct {
	Season, Type, Status, Reason string
	MembershipID                 int32
	NeedsReview                  bool
	Activities                   []dbsqlc.Activity
	Consents                     []dbsqlc.ListRegistrationApplicationConsentsRow
}
type RegistrationDetailView struct {
	Application  *RegistrationApplicationView
	AutomaticNew bool
	SecurityData
	SiteName, Title, Status, CreatedAt, Notice string
	ID                                         int32
	Open, Resolved                             bool
	EmailVerified, AwaitingEmail               bool
	Fields                                     []RegistrationFieldView
	Candidates                                 []RegistrationCandidateView
	ResolvedPersonID                           int32
	ResolutionType, ResolvedAt, Resolver       string
}

//go:embed templates/layouts/base.html templates/pages/registration_reviews.html templates/pages/registration_review_detail.html
var registrationFiles embed.FS
var registrationListTemplate = template.Must(template.ParseFS(registrationFiles, "templates/layouts/base.html", "templates/pages/registration_reviews.html"))
var registrationDetailTemplate = template.Must(template.ParseFS(registrationFiles, "templates/layouts/base.html", "templates/pages/registration_review_detail.html"))

func RenderRegistrationList(w io.Writer, v RegistrationListView) error {
	return registrationListTemplate.ExecuteTemplate(w, "base", v)
}
func RenderRegistrationDetail(w io.Writer, v RegistrationDetailView) error {
	return registrationDetailTemplate.ExecuteTemplate(w, "base", v)
}
func registrationStatus(s string) string {
	switch s {
	case "received":
		return "Reçue — aucun candidat détecté"
	case "awaiting_identity_review":
		return "À vérifier"
	case "awaiting_email_verification":
		return "Vérification email en cours"
	case "resolved":
		return "Résolue"
	case "cancelled":
		return "Annulée"
	}
	return "Statut inconnu"
}
func confidence(s string) string {
	switch s {
	case "strong":
		return "Fort (strong)"
	case "possible":
		return "Possible (possible)"
	case "weak":
		return "Faible (weak)"
	}
	return "Aucun"
}
func RegistrationRows(rows []dbsqlc.ListRegistrationReviewsRow, loc *time.Location) []RegistrationRowView {
	out := make([]RegistrationRowView, 0, len(rows))
	for _, r := range rows {
		row := RegistrationRowView{ID: r.ID, Name: r.LastName + " " + r.FirstName, BirthDate: date(r.BirthDate), CreatedAt: timestamp(r.CreatedAt, loc), Status: registrationStatus(r.Status), Confidence: confidence(r.BestConfidence), Count: r.CandidateCount, Awaiting: r.Status == "awaiting_identity_review"}
		if r.ApplicationNeedsReview {
			row.Status = "Identité résolue — adhésion à vérifier"
			row.Awaiting = true
		}
		out = append(out, row)
	}
	return out
}
func RegistrationDetail(d identityresolution.Details, loc *time.Location) RegistrationDetailView {
	s := d.Submission
	values := []string{s.FirstName, s.LastName, date(s.BirthDate), s.Email.String, s.PhoneNumber.String, s.Address.String}
	labels := []string{"Prénom", "Nom", "Date de naissance", "Email", "Téléphone", "Adresse"}
	v := RegistrationDetailView{ID: s.ID, Status: registrationStatus(s.Status), CreatedAt: timestamp(s.CreatedAt, loc), Open: s.Status == "received" || s.Status == "awaiting_identity_review" || s.Status == "awaiting_email_verification", EmailVerified: s.EmailVerified && !s.ResolvedByUserID.Valid, AwaitingEmail: s.Status == "awaiting_email_verification", Resolved: s.Status == "resolved", ResolvedPersonID: s.ResolvedPersonID.Int32, ResolvedAt: timestamp(s.ResolvedAt, loc), Resolver: textOrDash(s.ResolverUsername.String)}
	v.AutomaticNew = s.ResolutionType.String == "new_person" && !s.ResolvedByUserID.Valid
	if d.Application != nil {
		a := d.Application
		av := &RegistrationApplicationView{Season: a.SeasonName, Type: a.MembershipTypeName, MembershipID: a.MembershipID.Int32, NeedsReview: a.Status == "needs_review", Activities: d.Activities, Consents: d.Consents}
		switch a.Status {
		case "awaiting_identity":
			av.Status = "En attente de résolution de l’identité"
		case "membership_created":
			av.Status = "Demande d’adhésion enregistrée"
		case "needs_review":
			av.Status = "Vérification complémentaire nécessaire"
		case "cancelled":
			av.Status = "Annulée"
		}
		switch a.LastErrorCode.String {
		case "membership_already_exists":
			av.Reason = "Une adhésion existe déjà pour cette personne et cette saison."
		case "choices_unavailable":
			av.Reason = "La saison, le type d’adhésion ou une activité n’est plus disponible."
		case "member_not_adult":
			av.Reason = "L’identité retenue ne relève pas du parcours adulte."
		case "membership_unavailable":
			av.Reason = "La création de l’adhésion n’a pas pu aboutir. Une nouvelle tentative est possible."
		}
		v.Application = av
	}
	v.ResolutionType = "Person existante"
	if s.ResolutionType.String == "new_person" {
		v.ResolutionType = "Nouvelle Person"
	}
	for i, value := range values {
		v.Fields = append(v.Fields, RegistrationFieldView{Label: labels[i], Declared: textOrDash(value)})
	}
	for _, c := range d.Candidates {
		cv := RegistrationCandidateView{ID: c.PersonID, Name: c.FirstName + " " + c.LastName, Confidence: confidence(c.Confidence), DetectedAt: timestamp(c.DetectedAt, loc), Archived: c.ArchivedAt.Valid, Account: "Aucun User"}
		if c.UserID.Valid {
			cv.Account = "User actif — compte non activé"
			if c.ActivatedAt.Valid {
				cv.Account = "User actif — compte activé"
			}
			if !c.UserIsActive.Bool {
				cv.Account = "User désactivé"
				if !c.ActivatedAt.Valid {
					cv.Account += " — compte non activé"
				}
			}
		}
		for _, reason := range []struct {
			ok    bool
			label string
		}{{c.MatchedName, "Prénom et nom"}, {c.MatchedBirthDate, "Date de naissance exacte"}, {c.MatchedEmail, "Email normalisé"}, {c.MatchedPhone, "Téléphone normalisé"}} {
			if reason.ok {
				cv.Reasons = append(cv.Reasons, reason.label)
			}
		}
		existing := []string{c.FirstName, c.LastName, date(c.BirthDate), c.Email.String, c.PhoneNumber.String, c.Address.String}
		for i, value := range existing {
			cv.Fields = append(cv.Fields, RegistrationFieldView{Label: labels[i], Declared: textOrDash(values[i]), Existing: textOrDash(value), Different: values[i] != value})
		}
		v.Candidates = append(v.Candidates, cv)
	}
	return v
}
