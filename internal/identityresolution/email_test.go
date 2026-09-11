package identityresolution

import (
	"testing"
	"time"
)

func TestEmailServiceTTL(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Hour} {
		if _, err := NewEmailService(nil, ttl); err == nil {
			t.Fatal("nonpositive TTL accepted")
		}
	}
	if _, err := NewEmailService(nil, time.Minute); err != nil {
		t.Fatal(err)
	}
}
