package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePasswordInput struct {
	values []string
	calls  int
}

func (i *fakePasswordInput) ReadPassword(string) (string, error) {
	if i.calls >= len(i.values) {
		return "", errors.New("unexpected password read")
	}
	value := i.values[i.calls]
	i.calls++
	return value, nil
}

func validCLIEnvironment(password string) func(string) string {
	env := map[string]string{
		"BLOG_MYSQL_DSN":              "user:pass@tcp(mysql:3306)/blog",
		"BLOG_REDIS_ADDR":             "redis:6379",
		"BLOG_ADMIN_ORIGIN":           "http://localhost:3000",
		"BLOG_ADMIN_PASSWORD":         password,
		"BLOG_GFS_BASE_URL":           "http://gfs.example.com",
		"BLOG_GFS_APP_ID":             "blog-app",
		"BLOG_GFS_APP_SECRET":         "test-app-secret",
		"BLOG_GFS_PUBLIC_READ_SECRET": "test-public-read-secret",
		"BLOG_BUNDLE_TOKEN":           "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"BLOG_CALLBACK_HMAC_KEY":      "hhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhh",
		"BLOG_BUILDER_MASTER_KEY":     "a2tra2tra2tra2tra2tra2tra2tra2tra2tra2tra2s",
	}
	return func(key string) string { return env[key] }
}

func TestRunInitUsesEnvironmentPasswordAndPrintsOnlyIDAndUsername(t *testing.T) {
	secret := "automation-password"
	input := &fakePasswordInput{}
	var stdout strings.Builder
	var gotUsername, gotPassword string
	initialize := func(_ context.Context, _ config.Config, username, password string) (auth.Admin, error) {
		gotUsername, gotPassword = username, password
		return auth.Admin{ID: 41, Username: "MUST-NOT-PRINT", PasswordHash: "must-not-print", State: "active"}, nil
	}

	err := run([]string{"init", "--username", " Qiuxs "}, validCLIEnvironment(secret), input, &stdout, initialize)

	require.NoError(t, err)
	assert.Equal(t, "qiuxs", gotUsername)
	assert.Equal(t, secret, gotPassword)
	assert.Zero(t, input.calls)
	assert.Equal(t, "41 qiuxs\n", stdout.String())
	assert.NotContains(t, stdout.String(), secret)
	assert.NotContains(t, stdout.String(), "must-not-print")
}

func TestRunInitReadsAndConfirmsHiddenPassword(t *testing.T) {
	input := &fakePasswordInput{values: []string{"interactive-password", "interactive-password"}}
	var stdout strings.Builder
	initialize := func(_ context.Context, _ config.Config, username, password string) (auth.Admin, error) {
		assert.Equal(t, "qiuxs", username)
		assert.Equal(t, "interactive-password", password)
		return auth.Admin{ID: 42, Username: username}, nil
	}

	err := run([]string{"init", "--username", "qiuxs"}, validCLIEnvironment(""), input, &stdout, initialize)

	require.NoError(t, err)
	assert.Equal(t, 2, input.calls)
	assert.Equal(t, "42 qiuxs\n", stdout.String())
}

func TestRunInitRejectsPasswordMismatchWithoutInitialization(t *testing.T) {
	first, second := "first-password", "second-password"
	input := &fakePasswordInput{values: []string{first, second}}
	called := false
	initialize := func(context.Context, config.Config, string, string) (auth.Admin, error) {
		called = true
		return auth.Admin{}, nil
	}

	err := run([]string{"init", "--username", "qiuxs"}, validCLIEnvironment(""), input, &strings.Builder{}, initialize)

	assert.ErrorContains(t, err, "do not match")
	assert.NotContains(t, err.Error(), first)
	assert.NotContains(t, err.Error(), second)
	assert.False(t, called)
}

func TestRunInitDoesNotExposeEnvironmentPasswordInErrors(t *testing.T) {
	secret := "automation-password"
	initialize := func(context.Context, config.Config, string, string) (auth.Admin, error) {
		return auth.Admin{}, errors.New("redis allocation failed")
	}

	err := run([]string{"init", "--username", "qiuxs"}, validCLIEnvironment(secret), &fakePasswordInput{}, &strings.Builder{}, initialize)

	assert.Error(t, err)
	assert.NotContains(t, err.Error(), secret)
}

func TestExecuteRedactsDownstreamErrorDetails(t *testing.T) {
	password := "automation-password"
	fakeHash := "$argon2id$duplicate-secret-value"
	downstreamErr := errors.New("Duplicate entry '" + password + " " + fakeHash + "' for key 'uk_admins_username'")
	input := &fakePasswordInput{}
	var stdout, stderr strings.Builder
	initialize := func(context.Context, config.Config, string, string) (auth.Admin, error) {
		return auth.Admin{}, downstreamErr
	}

	exitCode := execute(
		[]string{"init", "--username", "qiuxs"},
		validCLIEnvironment(password),
		input,
		&stdout,
		&stderr,
		initialize,
	)

	assert.Equal(t, 1, exitCode)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "initialize administrator: operation failed\n", stderr.String())
	assert.NotContains(t, stderr.String(), password)
	assert.NotContains(t, stderr.String(), fakeHash)
}

func TestRunInitPreservesDownstreamErrorCause(t *testing.T) {
	downstreamErr := errors.New("database operation failed")
	initialize := func(context.Context, config.Config, string, string) (auth.Admin, error) {
		return auth.Admin{}, downstreamErr
	}

	internalErr := run(
		[]string{"init", "--username", "qiuxs"},
		validCLIEnvironment("automation-password"),
		&fakePasswordInput{},
		&strings.Builder{},
		initialize,
	)
	assert.ErrorIs(t, internalErr, downstreamErr)
}

func TestRunRejectsUnsupportedCommandBeforeLoadingConfiguration(t *testing.T) {
	getenvCalled := false

	err := run([]string{"delete"}, func(string) string { getenvCalled = true; return "" }, &fakePasswordInput{}, &strings.Builder{}, nil)

	assert.ErrorContains(t, err, "usage")
	assert.False(t, getenvCalled)
}
