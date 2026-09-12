package identityresolution

import (
	"strings"
	"testing"
	"time"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func txt(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
func TestMatchingRules(t *testing.T) {
	dob := pgtype.Date{Time: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	person := dbsqlc.ListIdentityMatchingPersonsRow{ID: 42, FirstName: "Rémi", LastName: "Du Pont", BirthDate: dob, Email: txt("known@example.test"), PhoneNumber: txt("+33 6 12 34 56 78")}
	for _, tc := range []struct {
		name, first, last, email, phone string
		birth                           pgtype.Date
		want                            string
		date, emailMatch, phoneMatch    bool
	}{
		{name: "name only", first: "Rémi", last: "Du Pont", want: "weak"},
		{name: "case spaces NFC", first: "  RE\u0301MI ", last: " du   PONT ", want: "weak"},
		{name: "birth exact", first: "Rémi", last: "Du Pont", birth: dob, want: "possible", date: true},
		{name: "email only with names", first: "Rémi", last: "Du Pont", email: " KNOWN@Example.Test ", want: "possible", emailMatch: true},
		{name: "phone French", first: "Rémi", last: "Du Pont", phone: "06.12.34.56.78", want: "possible", phoneMatch: true},
		{name: "strong email", first: "Rémi", last: "Du Pont", email: "known@example.test", birth: dob, want: "strong", date: true, emailMatch: true},
		{name: "strong international phone", first: "Rémi", last: "Du Pont", phone: "0033 (6) 12-34-56-78", birth: dob, want: "strong", date: true, phoneMatch: true},
		{name: "different birth stays possible", first: "Rémi", last: "Du Pont", email: "known@example.test", birth: pgtype.Date{Time: dob.Time.AddDate(0, 0, 1), Valid: true}, want: "possible", emailMatch: true},
		{name: "different name family email", first: "Anne", last: "Du Pont", email: "known@example.test", phone: "0612345678"},
		{name: "accent remains significant", first: "Remi", last: "Du Pont", birth: dob},
		{name: "punctuation remains significant", first: "Rémi", last: "Du-Pont", birth: dob},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, ok := match(SubmissionInput{FirstName: tc.first, LastName: tc.last, Email: txt(tc.email), PhoneNumber: txt(tc.phone), BirthDate: tc.birth}, person)
			if ok != (tc.want != "") || e.Confidence != tc.want || e.MatchedBirthDate != tc.date || e.MatchedEmail != tc.emailMatch || e.MatchedPhone != tc.phoneMatch {
				t.Fatalf("unexpected evidence: %+v", e)
			}
		})
	}
}
func TestPhoneNormalization(t *testing.T) {
	for input, want := range map[string]string{"": "", "123": "", "+33 6 12 34 56 78": "33612345678", "0612345678": "33612345678", "0033612345678": "33612345678", "+44 20 7946 0958": "442079460958", "06 12 34 56 78 ext 2": "", "06+12345678": ""} {
		if got := normalizedPhone(txt(input)); got != want {
			t.Errorf("%q: %q want %q", input, got, want)
		}
	}
}
func TestSubmissionValidation(t *testing.T) {
	for _, in := range []SubmissionInput{{}, {FirstName: " \t", LastName: "Name"}, {FirstName: strings.Repeat("a", 201), LastName: "Name"}, {FirstName: "A", LastName: "B", Email: txt(strings.Repeat("x", 255))}, {FirstName: "A", LastName: "B", BirthDate: pgtype.Date{Valid: true, InfinityModifier: pgtype.Infinity}}} {
		if ValidInput(in) {
			t.Fatal("invalid input accepted")
		}
	}
	if !ValidInput(SubmissionInput{FirstName: " A ", LastName: "B"}) {
		t.Fatal("optional birth date rejected")
	}
}
