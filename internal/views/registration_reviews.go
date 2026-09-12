package views

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/identityresolution"
	"github.com/jackc/pgx/v5/pgtype"
)

type RegistrationListView struct {
	SecurityData
	SiteName, Title string
	Rows            []RegistrationRowView
}
type RegistrationRowView struct {
	Kind, Age                                      string
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
	Location            *time.Location
	ActionReason        string
	CanRetryApplication bool
	Child               *identityresolution.ChildDetails
	Application         *RegistrationApplicationView
	AutomaticNew        bool
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
	return DisplayStatus(s).Label
}

func confidence(s string) string {
	switch s {
	case "strong":
		return "Fort"
	case "possible":
		return "Possible"
	case "weak":
		return "Faible"
	}
	return "Aucun"
}
func RegistrationRows(rows []dbsqlc.ListRegistrationReviewsRow, loc *time.Location) []RegistrationRowView {
	out := make([]RegistrationRowView, 0, len(rows))
	for _, r := range rows {
		row := RegistrationRowView{ID: r.ID, Name: r.LastName + " " + r.FirstName, BirthDate: date(r.BirthDate), CreatedAt: timestamp(r.CreatedAt, loc), Status: registrationStatus(r.Status), Confidence: confidence(r.BestConfidence), Count: r.CandidateCount, Awaiting: r.Status == "awaiting_identity_review" || r.Status == "received"}
		if r.ApplicationNeedsReview {
			row.Status = "Dossier d’adhésion à vérifier"
			switch r.ApplicationReason {
			case "guardian_confirmation_required":
				row.Status = "Lien avec le responsable à confirmer"
			case "guardian_identity_review":
				row.Status = "Identité du responsable à vérifier"
			case "child_identity_review":
				row.Status = "Identité de l’enfant à vérifier"
			}
			row.Awaiting = true
		}
		row.Kind = "Identité"
		switch r.ApplicationReason {
		case "guardian_confirmation_required":
			row.Kind = "Lien avec l’enfant"
		case "guardian_identity_review":
			row.Kind = "Identité du responsable"
		case "child_identity_review":
			row.Kind = "Identité de l’enfant"
		default:
			if r.ApplicationNeedsReview {
				row.Kind = "Adhésion"
			}
		}
		days := int(time.Since(r.CreatedAt.Time).Hours() / 24)
		row.Age = "Reçue aujourd’hui"
		if days > 0 {
			row.Age = fmt.Sprintf("Reçue il y a %d jour(s)", days)
		}
		out = append(out, row)
	}
	return out
}
func RegistrationDetail(d identityresolution.Details, loc *time.Location) RegistrationDetailView {
	s := d.Submission
	values := []string{s.FirstName, s.LastName, date(s.BirthDate), s.Email.String, s.PhoneNumber.String, s.Address.String}
	labels := []string{"Prénom", "Nom", "Date de naissance", "Email", "Téléphone", "Adresse"}
	v := RegistrationDetailView{Location: loc, Child: d.Child, ID: s.ID, Status: registrationStatus(s.Status), CreatedAt: timestamp(s.CreatedAt, loc), Open: s.Status == "received" || s.Status == "awaiting_identity_review" || s.Status == "awaiting_email_verification", EmailVerified: s.EmailVerified && !s.ResolvedByUserID.Valid, AwaitingEmail: s.Status == "awaiting_email_verification", Resolved: s.Status == "resolved", ResolvedPersonID: s.ResolvedPersonID.Int32, ResolvedAt: timestamp(s.ResolvedAt, loc), Resolver: textOrDash(s.ResolverUsername.String)}
	v.AutomaticNew = s.ResolutionType.String == "new_person" && !s.ResolvedByUserID.Valid
	if d.Application != nil {
		a := d.Application
		av := &RegistrationApplicationView{Season: a.SeasonName, Type: a.MembershipTypeName, MembershipID: a.MembershipID.Int32, NeedsReview: a.Status == "needs_review", Activities: d.Activities, Consents: d.Consents}
		av.Status = DisplayStatus(a.Status).Label
		switch a.LastErrorCode.String {
		case "membership_already_exists":
			av.Reason = "Une adhésion existe déjà pour cette personne et cette saison."
		case "choices_unavailable":
			av.Reason = "La saison, le type d’adhésion ou une activité n’est plus disponible."
		case "guardian_identity_review":
			av.Reason = "Identité du responsable à vérifier"
		case "child_identity_review":
			av.Reason = "Identité de l’enfant à vérifier"
		case "guardian_confirmation_required":
			av.Reason = "Lien avec le responsable à confirmer"
		case "member_not_minor":
			av.Reason = "La personne retenue pour l’enfant n’est pas mineure."
		case "guardian_relation_invalid":
			av.Reason = "La relation avec le responsable ne peut pas être utilisée. Vérifiez les fiches retenues."
		case "member_not_adult":
			av.Reason = "L’identité retenue ne relève pas du parcours adulte."
		case "membership_unavailable":
			av.Reason = "La création de l’adhésion n’a pas pu aboutir. Une nouvelle tentative est possible."
		}
		v.Application = av
	}
	v.ResolutionType = "Fiche existante"
	if s.ResolutionType.String == "new_person" {
		v.ResolutionType = "Nouvelle fiche"
	}
	for i, value := range values {
		v.Fields = append(v.Fields, RegistrationFieldView{Label: labels[i], Declared: textOrDash(value)})
	}
	for _, c := range d.Candidates {
		cv := RegistrationCandidateView{ID: c.PersonID, Name: c.FirstName + " " + c.LastName, Confidence: confidence(c.Confidence), DetectedAt: timestamp(c.DetectedAt, loc), Archived: c.ArchivedAt.Valid, Account: "Aucun compte"}
		if c.UserID.Valid {
			cv.Account = "Compte non activé"
			if c.ActivatedAt.Valid {
				cv.Account = "Compte activé"
			}
			if !c.UserIsActive.Bool {
				cv.Account = "Compte désactivé"
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
	if v.Open {
		v.ActionReason = "Vérifier l’identité de la personne concernée."
	}
	if v.AwaitingEmail {
		v.ActionReason = "Une vérification par email est en cours ; aucune intervention immédiate n’est nécessaire."
	}
	if v.Application != nil && v.Application.NeedsReview {
		v.ActionReason = v.Application.Reason
	}
	if d.Application != nil {
		code := d.Application.LastErrorCode.String
		v.CanRetryApplication = d.Application.Status == "needs_review" && code != "guardian_identity_review" && code != "child_identity_review" && code != "guardian_confirmation_required"
	}
	return v
}

func (v RegistrationDetailView) ReviewTime(t pgtype.Timestamptz) string {
	return timestamp(t, v.Location)
}
