package config

import (
	"testing"
	"time"
)

func TestPasswordResetTTL(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    time.Duration
		invalid bool
	}{
		{"", 30 * time.Minute, false}, {"45m", 45 * time.Minute, false}, {"0s", 0, true}, {"-1h", 0, true}, {"bad", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			cfg, err := loadRuntime(func(key string) string {
				if key == "PASSWORD_RESET_TTL" {
					return tc.value
				}
				return ""
			})
			if (err != nil) != tc.invalid || (!tc.invalid && (cfg.PasswordResetTTL != tc.want || cfg.ActivationValidity != 24*time.Hour)) {
				t.Fatal("reset TTL", cfg, err)
			}
		})
	}
}
