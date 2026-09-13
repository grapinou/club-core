package auth

import (
	"errors"
	"golang.org/x/crypto/bcrypt"
)

// ValidPassword is the existing activation policy: bytes, not rune count.
func ValidPassword(password string) bool { return len(password) >= 12 && len(password) <= 72 }
func HashPassword(password string) ([]byte, error) {
	if !ValidPassword(password) {
		return nil, errors.New("password must contain 12 to 72 bytes")
	}
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}
