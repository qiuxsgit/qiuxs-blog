package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAdminRepository struct {
	count        int
	countErr     error
	createErr    error
	createCalls  int
	username     string
	passwordHash string
	admin        auth.Admin
}

func (r *fakeAdminRepository) Count(context.Context) (int, error) {
	return r.count, r.countErr
}

func (r *fakeAdminRepository) Create(_ context.Context, username, passwordHash string) (auth.Admin, error) {
	r.createCalls++
	r.username = username
	r.passwordHash = passwordHash
	if r.createErr != nil {
		return auth.Admin{}, r.createErr
	}
	admin := r.admin
	admin.Username = username
	admin.PasswordHash = passwordHash
	admin.State = "active"
	return admin, nil
}

func (*fakeAdminRepository) FindByUsername(context.Context, string) (auth.Admin, error) {
	panic("unexpected FindByUsername")
}

func (*fakeAdminRepository) FindByID(context.Context, int64) (auth.Admin, error) {
	panic("unexpected FindByID")
}

func (*fakeAdminRepository) UpdateLastLogin(context.Context, int64, time.Time) error {
	panic("unexpected UpdateLastLogin")
}

func TestCreateFirstAdminCreatesActiveAdminWithArgon2idHash(t *testing.T) {
	repo := &fakeAdminRepository{admin: auth.Admin{ID: 17}}
	hasher := auth.DefaultPasswordHasher()

	admin, err := CreateFirstAdmin(context.Background(), repo, hasher, "  Qiuxs  ", "long-enough-password")

	require.NoError(t, err)
	assert.Equal(t, auth.Admin{ID: 17, Username: "qiuxs", State: "active"}, admin)
	assert.Equal(t, 1, repo.createCalls)
	assert.Equal(t, "qiuxs", repo.username)
	assert.True(t, strings.HasPrefix(repo.passwordHash, "$argon2id$"))
	matched, verifyErr := hasher.Verify("long-enough-password", repo.passwordHash)
	require.NoError(t, verifyErr)
	assert.True(t, matched)
}

func TestCreateFirstAdminReturnsExistsWithoutHashingOrWriting(t *testing.T) {
	repo := &fakeAdminRepository{count: 1}

	admin, err := CreateFirstAdmin(context.Background(), repo, auth.PasswordHasher{}, "qiuxs", "long-enough-password")

	assert.Empty(t, admin)
	assert.ErrorIs(t, err, ErrAdminExists)
	assert.Zero(t, repo.createCalls)
}

func TestCreateFirstAdminRejectsInvalidInputBeforeHashingOrWriting(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "invalid username", username: "bad name", password: "long-enough-password"},
		{name: "short password", username: "qiuxs", password: "12345678901"},
		{name: "short UTF-8 password in bytes", username: "qiuxs", password: "密码密"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeAdminRepository{}

			admin, err := CreateFirstAdmin(context.Background(), repo, auth.PasswordHasher{}, tt.username, tt.password)

			assert.Empty(t, admin)
			assert.Error(t, err)
			assert.Zero(t, repo.createCalls)
			assert.NotContains(t, err.Error(), tt.password)
		})
	}
}

func TestCreateFirstAdminMapsUniqueRaceToAdminExists(t *testing.T) {
	repo := &fakeAdminRepository{createErr: auth.ErrAdminAlreadyExists}

	admin, err := CreateFirstAdmin(context.Background(), repo, auth.DefaultPasswordHasher(), "qiuxs", "long-enough-password")

	assert.Empty(t, admin)
	assert.ErrorIs(t, err, ErrAdminExists)
	assert.Equal(t, 1, repo.createCalls)
}

func TestCreateFirstAdminPropagatesRepositoryErrors(t *testing.T) {
	dependencyErr := errors.New("database unavailable")
	repo := &fakeAdminRepository{countErr: dependencyErr}

	_, err := CreateFirstAdmin(context.Background(), repo, auth.PasswordHasher{}, "qiuxs", "long-enough-password")

	assert.ErrorIs(t, err, dependencyErr)
}
