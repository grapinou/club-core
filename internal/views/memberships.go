package views

import (
	"embed"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/memberships"
	"github.com/jackc/pgx/v5/pgtype"
)

// These presentation types deliberately contain no password hash, delivery code,
// session token or raw User model.
type MembershipListView struct {
	SecurityData
	SiteName, Title string
	Rows            []MembershipRowView
}
type MembershipRowView struct {
	ID                                                                                      int32
	Name, Season, Type, Status, StatusClass, RequestedAt, ApprovedAt, Completeness, Account string
	Pending                                                                                 bool
	BlockingCount, WarningCount                                                             int
}
type ContactView struct {
	Name, Relationship, Phone, Email string
	Primary                          bool
	Priority                         int32
}
type ConsentView struct {
	Title, Description, Decision, Class, Giver, RecordedAt string
	Version                                                int32
}
type AccountView struct {
	Exists, Active, Activated, NeedsActivation bool
	ID                                         int32
	Username, Label                            string
}
type MembershipDetailView struct {
	SecurityData
	SiteName, Title, Notice, NoticeClass                                                      string
	ID                                                                                        int32
	FirstName, LastName, BirthDate, Majority, Phone, Email, Address, PersonNotes              string
	Season, Type, Status, StatusClass, RequestedAt, JoinedAt, ApprovedAt, Approver, AdminNote string
	HasApproval, Pending, Ready, CanApprove, CanResend                                        bool
	CompletenessTitle                                                                         string
	BlockingIssues, Warnings, Activities                                                      []string
	Guardians, EmergencyContacts                                                              []ContactView
	Consents                                                                                  []ConsentView
	Account                                                                                   AccountView
}

//go:embed templates/layouts/base.html templates/pages/memberships.html templates/pages/membership_detail.html
var membershipFiles embed.FS
var membershipListTemplate = template.Must(template.ParseFS(membershipFiles, "templates/layouts/base.html", "templates/pages/memberships.html"))
var membershipDetailTemplate = template.Must(template.ParseFS(membershipFiles, "templates/layouts/base.html", "templates/pages/membership_detail.html"))

func RenderMembershipList(w io.Writer, data MembershipListView) error {
	return membershipListTemplate.ExecuteTemplate(w, "base", data)
}
func RenderMembershipDetail(w io.Writer, data MembershipDetailView) error {
	return membershipDetailTemplate.ExecuteTemplate(w, "base", data)
}

func textOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
func date(v pgtype.Date) string {
	if !v.Valid || v.InfinityModifier != pgtype.Finite {
		return "—"
	}
	return v.Time.Format("02/01/2006")
}
func timestamp(v pgtype.Timestamptz, loc *time.Location) string {
	if !v.Valid || v.InfinityModifier != pgtype.Finite {
		return "—"
	}
	return v.Time.In(loc).Format("02/01/2006 à 15:04")
}
func membershipStatus(s string) (string, string) {
	switch s {
	case "pending":
		return "En attente (pending)", "text-bg-warning"
	case "active":
		return "Active (active)", "text-bg-success"
	case "ended":
		return "Terminée (ended)", "text-bg-secondary"
	case "cancelled":
		return "Annulée (cancelled)", "text-bg-secondary"
	}
	return "Statut inconnu", "text-bg-secondary"
}
func accountLabel(a memberships.AccountState) string {
	if !a.Exists {
		return "Aucun compte"
	}
	if !a.IsActive {
		return "Compte désactivé"
	}
	if a.IsActivated {
		return "Compte activé"
	}
	return "Activation nécessaire"
}
func issueLabel(code string) string {
	switch code {
	case "missing_birth_date":
		return "Date de naissance manquante"
	case "missing_activity":
		return "Aucune activité renseignée"
	case "minor_missing_guardian":
		return "Responsable légal manquant pour un mineur"
	case "minor_missing_emergency":
		return "Contact d'urgence manquant pour un mineur"
	case "unanswered_consent":
		return "Une autorisation n'a pas reçu de réponse"
	case "adult_missing_emergency":
		return "Contact d'urgence conseillé"
	}
	return "Le dossier nécessite une vérification."
}
func relationship(s string) string {
	switch s {
	case "mother":
		return "Mère"
	case "father":
		return "Père"
	case "guardian":
		return "Responsable légal"
	case "other":
		return "Autre"
	}
	return textOrDash(s)
}
func MembershipRows(entries []memberships.ListEntry, loc *time.Location) []MembershipRowView {
	rows := make([]MembershipRowView, 0, len(entries))
	for _, e := range entries {
		m := e.Membership
		status, class := membershipStatus(m.Membership.Status)
		complete := "Complet"
		if len(e.Completeness.BlockingIssues) > 0 {
			complete = "À compléter"
		}
		rows = append(rows, MembershipRowView{ID: m.Membership.ID, Name: m.LastName + " " + m.FirstName, Season: m.SeasonName, Type: m.MembershipTypeName, Status: status, StatusClass: class, RequestedAt: timestamp(m.Membership.RequestedAt, loc), ApprovedAt: timestamp(m.Membership.ApprovedAt, loc), Completeness: complete, Account: accountLabel(e.Account), Pending: m.Membership.Status == "pending", BlockingCount: len(e.Completeness.BlockingIssues), WarningCount: len(e.Completeness.Warnings)})
	}
	return rows
}
func MembershipDetail(d memberships.Details, loc *time.Location, approve, resend bool) MembershipDetailView {
	row := d.Membership
	m, p := row.Membership, row.Person
	status, class := membershipStatus(m.Status)
	v := MembershipDetailView{ID: m.ID, FirstName: p.FirstName, LastName: p.LastName, BirthDate: date(p.BirthDate), Majority: "Date de naissance inconnue",
		Phone: textOrDash(p.PhoneNumber.String), Email: textOrDash(p.Email.String), Address: textOrDash(p.Address.String), PersonNotes: textOrDash(p.Notes.String),
		Season: row.Season.Name, Type: row.MembershipType.Name, Status: status, StatusClass: class, RequestedAt: timestamp(m.RequestedAt, loc), JoinedAt: date(m.JoinedAt), ApprovedAt: timestamp(m.ApprovedAt, loc), Approver: textOrDash(row.ApproverUsername.String), AdminNote: m.AdminNote.String, HasApproval: m.ApprovedAt.Valid,
		Pending: m.Status == "pending", Ready: len(d.Completeness.BlockingIssues) == 0}
	if d.Completeness.IsMinor != nil {
		v.Majority = "Majeur"
		if *d.Completeness.IsMinor {
			v.Majority = "Mineur"
		}
	}
	v.CompletenessTitle = "Dossier prêt à être validé"
	if !v.Pending {
		v.CompletenessTitle = "Dossier complet"
	}
	if !v.Ready {
		v.CompletenessTitle = "Validation impossible"
	}
	for _, issue := range d.Completeness.BlockingIssues {
		v.BlockingIssues = append(v.BlockingIssues, issueLabel(issue))
	}
	for _, warning := range d.Completeness.Warnings {
		v.Warnings = append(v.Warnings, issueLabel(warning))
	}
	for _, a := range d.Activities {
		v.Activities = append(v.Activities, a.Name)
	}
	for _, g := range d.Guardians {
		v.Guardians = append(v.Guardians, ContactView{Name: g.FirstName + " " + g.LastName, Relationship: relationship(g.RelationshipType), Primary: g.IsPrimaryContact, Phone: textOrDash(g.PhoneNumber.String), Email: textOrDash(g.Email.String)})
	}
	for _, e := range d.EmergencyContacts {
		v.EmergencyContacts = append(v.EmergencyContacts, ContactView{Name: e.FirstName + " " + e.LastName, Relationship: textOrDash(e.RelationshipLabel.String), Phone: textOrDash(e.PhoneNumber.String), Email: textOrDash(e.Email.String), Priority: e.Priority})
	}
	for _, c := range d.ConsentRequirements {
		decision, class := "Non renseigné", "text-bg-warning"
		switch c.Decision.String {
		case "granted":
			decision, class = "Accordé (granted)", "text-bg-success"
		case "refused":
			decision, class = "Refusé (refused)", "text-bg-secondary"
		case "withdrawn":
			decision, class = "Retiré (withdrawn)", "text-bg-secondary"
		}
		v.Consents = append(v.Consents, ConsentView{Title: c.Title, Description: c.Description, Version: c.Version, Decision: decision, Class: class, Giver: textOrDash(strings.TrimSpace(c.GiverFirstName.String + " " + c.GiverLastName.String)), RecordedAt: timestamp(c.RecordedAt, loc)})
	}
	v.Account = AccountView{Exists: d.Account.Exists, Active: d.Account.IsActive, Activated: d.Account.IsActivated, NeedsActivation: d.Account.NeedsActivation, ID: row.UserID.Int32, Username: row.Username.String, Label: accountLabel(d.Account)}
	v.CanApprove = approve && v.Pending
	v.CanResend = resend && d.Account.Exists && d.Account.IsActive && d.Account.NeedsActivation
	return v
}
