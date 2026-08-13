package config_test

import (
	"testing"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLoadProductionConfig(t *testing.T) {
	env := validEnv()

	got, err := config.Load(func(key string) string { return env[key] })

	require.NoError(t, err)
	require.Equal(t, "production", got.Environment)
	require.Equal(t, ":9010", got.HTTP.Addr)
	require.Equal(t, "https://blog-admin.qiuxs.com", got.HTTP.AdminOrigin)
	require.Equal(t, "blog:secret@tcp(mysql:3306)/qiuxs_blog?parseTime=true&loc=UTC", got.MySQL.DSN)
	require.Equal(t, "redis:6379", got.Redis.Addr)
	require.Equal(t, "redis-secret", got.Redis.Password)
	require.Equal(t, 2, got.Redis.DB)
	require.Equal(t, int64(1), got.IDGen.Offset)
	require.Equal(t, int64(1), got.IDGen.Step)
	require.False(t, got.IDGen.Heal)
	require.Equal(t, "qx_blog_session", got.Session.CookieName)
	require.True(t, got.Session.CookieSecure)
	require.Equal(t, 24*time.Hour, got.Session.TTL)
}

func TestLoadUsesDevelopmentDefaults(t *testing.T) {
	env := validEnv()
	delete(env, "BLOG_ENV")
	delete(env, "BLOG_HTTP_ADDR")
	delete(env, "BLOG_REDIS_DB")
	delete(env, "IDGEN_OFFSET")
	delete(env, "IDGEN_STEP")
	delete(env, "IDGEN_HEAL")
	delete(env, "BLOG_SESSION_COOKIE_NAME")
	delete(env, "BLOG_SESSION_TTL")

	got, err := config.Load(func(key string) string { return env[key] })

	require.NoError(t, err)
	require.Equal(t, "development", got.Environment)
	require.Equal(t, ":8080", got.HTTP.Addr)
	require.Equal(t, 0, got.Redis.DB)
	require.Equal(t, int64(1), got.IDGen.Offset)
	require.Equal(t, int64(1), got.IDGen.Step)
	require.False(t, got.IDGen.Heal)
	require.Equal(t, "qx_blog_session", got.Session.CookieName)
	require.False(t, got.Session.CookieSecure)
	require.Equal(t, 24*time.Hour, got.Session.TTL)
}

func TestLoadRejectsMissingMySQLDSN(t *testing.T) {
	env := validEnv()
	delete(env, "BLOG_MYSQL_DSN")

	_, err := config.Load(func(key string) string { return env[key] })

	require.ErrorContains(t, err, "BLOG_MYSQL_DSN")
}

func TestLoadRejectsMissingRedisAddress(t *testing.T) {
	env := validEnv()
	delete(env, "BLOG_REDIS_ADDR")

	_, err := config.Load(func(key string) string { return env[key] })

	require.ErrorContains(t, err, "BLOG_REDIS_ADDR")
}

func TestLoadRejectsMalformedRedisDB(t *testing.T) {
	env := validEnv()
	env["BLOG_REDIS_DB"] = "two"

	_, err := config.Load(func(key string) string { return env[key] })

	require.ErrorContains(t, err, "BLOG_REDIS_DB")
}

func TestLoadRejectsInvalidSessionTTL(t *testing.T) {
	env := validEnv()
	env["BLOG_SESSION_TTL"] = "14m"

	_, err := config.Load(func(key string) string { return env[key] })

	require.ErrorContains(t, err, "BLOG_SESSION_TTL")
}

func TestLoadRejectsNonHTTPSProductionOrigin(t *testing.T) {
	env := validEnv()
	env["BLOG_ADMIN_ORIGIN"] = "http://blog-admin.qiuxs.com"

	_, err := config.Load(func(key string) string { return env[key] })

	require.ErrorContains(t, err, "https")
}

func TestLoadRejectsInvalidIDGeneratorRange(t *testing.T) {
	tests := []struct {
		name   string
		offset string
		step   string
	}{
		{name: "zero offset", offset: "0", step: "1"},
		{name: "negative offset", offset: "-1", step: "1"},
		{name: "zero step", offset: "1", step: "0"},
		{name: "offset exceeds step", offset: "2", step: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validEnv()
			env["IDGEN_OFFSET"] = tt.offset
			env["IDGEN_STEP"] = tt.step

			_, err := config.Load(func(key string) string { return env[key] })

			require.ErrorContains(t, err, "IDGEN")
		})
	}
}

func TestLoadRejectsMalformedIDGeneratorHealFlag(t *testing.T) {
	env := validEnv()
	env["IDGEN_HEAL"] = "sometimes"

	_, err := config.Load(func(key string) string { return env[key] })

	require.ErrorContains(t, err, "IDGEN_HEAL")
}

func TestLoadNormalizesAndValidatesAdminOrigin(t *testing.T) {
	tests := []struct {
		name    string
		origin  string
		want    string
		wantErr string
	}{
		{name: "normalizes root slash", origin: "http://localhost:3000/", want: "http://localhost:3000"},
		{name: "requires an origin", origin: "", wantErr: "BLOG_ADMIN_ORIGIN"},
		{name: "rejects userinfo", origin: "https://user@blog-admin.qiuxs.com", wantErr: "BLOG_ADMIN_ORIGIN"},
		{name: "rejects query", origin: "https://blog-admin.qiuxs.com?preview=true", wantErr: "BLOG_ADMIN_ORIGIN"},
		{name: "rejects fragment", origin: "https://blog-admin.qiuxs.com#top", wantErr: "BLOG_ADMIN_ORIGIN"},
		{name: "rejects non-root path", origin: "https://blog-admin.qiuxs.com/admin", wantErr: "BLOG_ADMIN_ORIGIN"},
		{name: "rejects unsupported scheme", origin: "ftp://blog-admin.qiuxs.com", wantErr: "BLOG_ADMIN_ORIGIN"},
		{name: "rejects missing host", origin: "https:", wantErr: "BLOG_ADMIN_ORIGIN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validEnv()
			env["BLOG_ENV"] = "development"
			env["BLOG_ADMIN_ORIGIN"] = tt.origin

			got, err := config.Load(func(key string) string { return env[key] })

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.HTTP.AdminOrigin)
		})
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"BLOG_ENV":                 "production",
		"BLOG_HTTP_ADDR":           ":9010",
		"BLOG_MYSQL_DSN":           "blog:secret@tcp(mysql:3306)/qiuxs_blog?parseTime=true&loc=UTC",
		"BLOG_REDIS_ADDR":          "redis:6379",
		"BLOG_REDIS_PASSWORD":      "redis-secret",
		"BLOG_REDIS_DB":            "2",
		"IDGEN_OFFSET":             "1",
		"IDGEN_STEP":               "1",
		"IDGEN_HEAL":               "false",
		"BLOG_ADMIN_ORIGIN":        "https://blog-admin.qiuxs.com",
		"BLOG_SESSION_COOKIE_NAME": "qx_blog_session",
		"BLOG_SESSION_TTL":         "24h",
	}
}
