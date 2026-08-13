package auth

import (
	"context"
	"time"
)

// Repository persists the single administrator account.
type Repository interface {
	Count(ctx context.Context) (int, error)
	Create(ctx context.Context, username, passwordHash string) (Admin, error)
	FindByUsername(ctx context.Context, username string) (Admin, error)
	FindByID(ctx context.Context, id int64) (Admin, error)
	UpdateLastLogin(ctx context.Context, id int64, at time.Time) error
}
