package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
)

const minimumAdminPasswordBytes = 12

var ErrAdminExists = errors.New("administrator already exists")

// CreateFirstAdmin creates the only administrator account allowed by the
// service. The database singleton constraint remains the final authority when
// concurrent initializers race after Count.
func CreateFirstAdmin(
	ctx context.Context,
	repo auth.Repository,
	hasher auth.PasswordHasher,
	username string,
	password string,
) (auth.Admin, error) {
	if repo == nil {
		return auth.Admin{}, errors.New("admin repository is required")
	}

	count, err := repo.Count(ctx)
	if err != nil {
		return auth.Admin{}, fmt.Errorf("count administrators: %w", err)
	}
	if count != 0 {
		return auth.Admin{}, ErrAdminExists
	}

	normalized, err := auth.NormalizeUsername(username)
	if err != nil {
		return auth.Admin{}, err
	}
	if len(password) < minimumAdminPasswordBytes {
		return auth.Admin{}, errors.New("admin password must be at least 12 bytes")
	}

	passwordHash, err := hasher.Hash(password)
	if err != nil {
		return auth.Admin{}, fmt.Errorf("hash admin password: %w", err)
	}
	admin, err := repo.Create(ctx, normalized, passwordHash)
	if err != nil {
		if errors.Is(err, auth.ErrAdminAlreadyExists) {
			return auth.Admin{}, ErrAdminExists
		}
		return auth.Admin{}, fmt.Errorf("create administrator: %w", err)
	}
	admin.PasswordHash = ""
	return admin, nil
}
