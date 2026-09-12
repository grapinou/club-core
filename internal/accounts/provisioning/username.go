package provisioning

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// UsernameBase never depends on season, age, email or practice group.
func UsernameBase(first, last string) string {
	clean := func(s string) string {
		s = strings.NewReplacer("œ", "oe", "æ", "ae", "ß", "ss", "ø", "o", "ł", "l").Replace(strings.ToLower(s))
		var b strings.Builder
		separator := false
		for _, r := range norm.NFD.String(s) {
			if unicode.Is(unicode.Mn, r) {
				continue
			}
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				if separator && b.Len() > 0 {
					b.WriteByte('.')
				}
				separator = false
				b.WriteRune(r)
			} else {
				separator = true
			}
		}
		return b.String()
	}
	name := strings.Trim(clean(first)+"."+clean(last), ".")
	if name == "" {
		return "member"
	}
	return name
}
