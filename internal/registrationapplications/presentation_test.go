package registrationapplications

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPresentationAuthenticationAndExpiry(t *testing.T) {
	s := &Service{}
	token, err := s.Present(Catalog{}, "browser")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.presentation(token, "browser")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.presentation(token, "other browser"); err == nil {
		t.Fatal("browser binding missing")
	}
	if _, err = s.presentation(token+"x", "browser"); err == nil {
		t.Fatal("tampered signature")
	}
	if _, err = s.presentation(strings.Repeat("x", 7000), "browser"); err == nil {
		t.Fatal("unbounded token")
	}
	for _, at := range []int64{time.Now().Add(-PresentationValidity - time.Minute).Unix(), time.Now().Add(time.Minute).Unix()} {
		p.At = at
		body, _ := json.Marshal(p)
		mac := hmac.New(sha256.New, s.key[:])
		mac.Write(body)
		expired := base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		if _, err = s.presentation(expired, "browser"); err == nil {
			t.Fatal("invalid presentation lifetime")
		}
	}
	next, err := s.Present(Catalog{}, "browser")
	if err != nil {
		t.Fatal(err)
	}
	if token == next {
		t.Fatal("form request keys reused")
	}
}
