package views

import (
	"embed"
	"html/template"
	"io"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
)

type IdentityCorrectionRow struct {
	ID                                int32
	Name, Username, CreatedAt, Status string
}
type IdentityCorrectionView struct {
	SecurityData
	SiteName, Title, Mode, FirstName, LastName, BirthDate, Error, Message string
	Pending                                                               bool
	Errors                                                                map[string]string
	Rows                                                                  []IdentityCorrectionRow
	ID, PersonID, RequestingUserID                                        int32
	Username, CreatedAt, ReviewedAt, Status                               string
	Open                                                                  bool
	Fields                                                                []RegistrationFieldView
}

func CorrectionStatus(status string) string {
	switch status {
	case "pending":
		return "En attente"
	case "approved":
		return "Validée"
	case "rejected":
		return "Refusée"
	}
	return ""
}
func CorrectionRows(rows []dbsqlc.ListIdentityCorrectionsRow, loc *time.Location) []IdentityCorrectionRow {
	result := make([]IdentityCorrectionRow, 0, len(rows))
	for _, r := range rows {
		result = append(result, IdentityCorrectionRow{ID: r.ID, Name: r.FirstName + " " + r.LastName, Username: r.Username, CreatedAt: timestamp(r.CreatedAt, loc), Status: CorrectionStatus(r.Status)})
	}
	return result
}
func CorrectionDetail(r dbsqlc.GetIdentityCorrectionRow, loc *time.Location) IdentityCorrectionView {
	v := IdentityCorrectionView{Mode: "detail", ID: r.ID, PersonID: r.PersonID, RequestingUserID: r.RequestingUserID, Username: r.Username, CreatedAt: timestamp(r.CreatedAt, loc), Status: CorrectionStatus(r.Status), Open: r.Status == "pending"}
	if r.ReviewedAt.Valid {
		v.ReviewedAt = timestamp(r.ReviewedAt, loc)
	}
	current := []string{r.FirstName, r.LastName, date(r.BirthDate)}
	proposed := []string{r.ProposedFirstName, r.ProposedLastName, date(r.ProposedBirthDate)}
	for i, label := range []string{"Prénom", "Nom", "Date de naissance"} {
		v.Fields = append(v.Fields, RegistrationFieldView{Label: label, Existing: textOrDash(current[i]), Declared: textOrDash(proposed[i]), Different: current[i] != proposed[i]})
	}
	return v
}

//go:embed templates/layouts/base.html templates/pages/identity_corrections.html
var correctionFiles embed.FS
var correctionTemplate = template.Must(template.ParseFS(correctionFiles, "templates/layouts/base.html", "templates/pages/identity_corrections.html"))

func RenderIdentityCorrection(w io.Writer, v IdentityCorrectionView) error {
	return correctionTemplate.ExecuteTemplate(w, "base", v)
}
