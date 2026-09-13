// Package auth authenticates durable users independently of memberships.
package auth

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"golang.org/x/crypto/bcrypt"
)

var ErrCredentials = errors.New("Identifiant ou mot de passe incorrect.")

type Users interface {
	GetUserByUsername(context.Context, string) (dbsqlc.User, error)
	GetUserByID(context.Context, int32) (dbsqlc.User, error)
}
type Service struct {
	users     Users
	dummyHash []byte
}

func New(users Users) (*Service, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte("dummy comparison only"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Service{users: users, dummyHash: hash}, nil
}
func eligible(u dbsqlc.User) bool { return u.IsActive && u.ActivatedAt.Valid && u.PasswordHash.Valid }
func (s *Service) Authenticate(ctx context.Context, username, password string) (int32, error) {
	id, _, err := s.AuthenticateSession(ctx, username, password)
	return id, err
}

// AuthenticateSession binds a login to the exact credential that was checked.
func (s *Service) AuthenticateSession(ctx context.Context, username, password string) (int32, [32]byte, error) {
	user, err := s.users.GetUserByUsername(ctx, username)
	valid := err == nil && eligible(user)
	hash := s.dummyHash
	if valid {
		hash = []byte(user.PasswordHash.String)
	}
	if len(password) > 72 {
		password = "invalid length"
		valid = false
		hash = s.dummyHash
	}
	check := bcrypt.CompareHashAndPassword(hash, []byte(password))
	if !valid || check != nil {
		return 0, [32]byte{}, ErrCredentials
	}
	return user.ID, sha256.Sum256([]byte(user.PasswordHash.String)), nil
}
func (s *Service) SessionUser(ctx context.Context, id int32) bool {
	user, err := s.users.GetUserByID(ctx, id)
	return err == nil && eligible(user)
}

func (s *Service) sessionCredential(ctx context.Context, id int32, credential [32]byte) bool {
	user, err := s.users.GetUserByID(ctx, id)
	if err != nil || !eligible(user) {
		return false
	}
	return credential == ([32]byte{}) || credential == sha256.Sum256([]byte(user.PasswordHash.String))
}
