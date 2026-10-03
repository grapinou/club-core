package civildate

import (
	"testing"
	"time"
)

func TestParseFrenchStrict(t *testing.T) {
	for _, tt := range []struct{ value, iso string }{
		{"03/10/2026", "2026-10-03"}, {"29/02/2024", "2024-02-29"}, {"17/04/2012", "2012-04-17"}, {"01/01/0001", "0001-01-01"},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParseFrench(tt.value)
			if err != nil || got.Format("2006-01-02") != tt.iso || got.Location() != time.UTC {
				t.Fatalf("%v %v", got, err)
			}
			if FormatFrench(got) != tt.value {
				t.Fatal("format round trip")
			}
		})
	}
	for _, value := range []string{"29/02/2025", "31/04/2020", "31/02/2020", "3/10/2026", "03/1/2026", "2026-10-03", "", "bonjour", " 03/10/2026", "03/10/2026 ", "01/01/0000", "０３/１０/２０２６", "03/10/26", "03-10-2026"} {
		t.Run("invalid "+value, func(t *testing.T) {
			if _, err := ParseFrench(value); err == nil {
				t.Fatal("accepted invalid date")
			}
		})
	}
	t0 := time.Date(2012, 4, 17, 0, 0, 0, 0, time.FixedZone("Paris", 7200))
	if FormatFrench(t0) != "17/04/2012" {
		t.Fatal("format changed civil date")
	}
}
