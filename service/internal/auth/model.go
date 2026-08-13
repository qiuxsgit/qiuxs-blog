package auth

import "errors"

var (
	ErrAdminNotFound      = errors.New("admin not found")
	ErrAdminAlreadyExists = errors.New("admin already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInternal           = errors.New("internal error")
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrRateLimited        = errors.New("rate limited")
	ErrSessionNotFound    = errors.New("session not found")
)

type Admin struct {
	ID           int64
	Username     string
	PasswordHash string
	State        string
}
