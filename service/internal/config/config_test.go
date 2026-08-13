package config_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	require.Equal(t, "https://gfs.example.com", got.GFS.BaseURL)
	require.Equal(t, "blog-app", got.GFS.AppID)
	require.Equal(t, "raw-app-secret", got.GFS.AppSecret)
	require.Equal(t, "public-read-secret", got.GFS.PublicReadSecret)
	require.Equal(t, []byte(strings.Repeat("b", 32)), got.Release.BundleToken)
	require.Equal(t, []byte(strings.Repeat("h", 32)), got.Release.CallbackHMACKey)
	require.Equal(t, []byte(strings.Repeat("k", 32)), got.Release.BuilderMasterKey)
	require.Equal(t, "/srv/blog/current/release.json", got.Release.CurrentReleaseJSONPath)
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
	delete(env, "BLOG_CURRENT_RELEASE_JSON_PATH")

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
	require.Equal(t, "/web/deploy/blog-site/current/release.json", got.Release.CurrentReleaseJSONPath)
}

func TestLoadReleaseSecretsAcceptsExactBoundsAndDoesNotProbeArtifactPath(t *testing.T) {
	for _, size := range []int{32, 128} {
		t.Run("size_"+strconv.Itoa(size), func(t *testing.T) {
			env := validEnv()
			env["BLOG_BUNDLE_TOKEN"] = strings.Repeat("b", size)
			env["BLOG_CALLBACK_HMAC_KEY"] = strings.Repeat("h", size)
			env["BLOG_BUILDER_MASTER_KEY"] = base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
			env["BLOG_CURRENT_RELEASE_JSON_PATH"] = filepath.Join(t.TempDir(), "missing", "release.json")

			got, err := config.Load(func(key string) string { return env[key] })

			require.NoError(t, err)
			require.Len(t, got.Release.BundleToken, size)
			require.Len(t, got.Release.CallbackHMACKey, size)
			_, statErr := os.Stat(env["BLOG_CURRENT_RELEASE_JSON_PATH"])
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func TestLoadReleaseConfigDoesNotAliasEnvironmentValues(t *testing.T) {
	env := validEnv()
	got, err := config.Load(func(key string) string { return env[key] })
	require.NoError(t, err)

	env["BLOG_BUNDLE_TOKEN"] = strings.Repeat("x", 32)
	env["BLOG_CALLBACK_HMAC_KEY"] = strings.Repeat("y", 32)
	env["BLOG_BUILDER_MASTER_KEY"] = base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("z", 32)))
	require.Equal(t, []byte(strings.Repeat("b", 32)), got.Release.BundleToken)
	require.Equal(t, []byte(strings.Repeat("h", 32)), got.Release.CallbackHMACKey)
	require.Equal(t, []byte(strings.Repeat("k", 32)), got.Release.BuilderMasterKey)
}

func TestLoadRejectsInvalidReleaseSecretsWithoutEchoingThem(t *testing.T) {
	master := base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("m", 32)))
	tests := []struct {
		name, key, value string
		remove           bool
	}{
		{name: "missing bundle token", key: "BLOG_BUNDLE_TOKEN", remove: true},
		{name: "short bundle token", key: "BLOG_BUNDLE_TOKEN", value: "bundle-secret-short"},
		{name: "blank bundle token", key: "BLOG_BUNDLE_TOKEN", value: strings.Repeat(" ", 32)},
		{name: "long bundle token", key: "BLOG_BUNDLE_TOKEN", value: strings.Repeat("B", 129)},
		{name: "missing callback key", key: "BLOG_CALLBACK_HMAC_KEY", remove: true},
		{name: "short callback key", key: "BLOG_CALLBACK_HMAC_KEY", value: "callback-secret-short"},
		{name: "blank callback key", key: "BLOG_CALLBACK_HMAC_KEY", value: strings.Repeat("\t", 32)},
		{name: "long callback key", key: "BLOG_CALLBACK_HMAC_KEY", value: strings.Repeat("H", 129)},
		{name: "missing builder key", key: "BLOG_BUILDER_MASTER_KEY", remove: true},
		{name: "malformed builder key", key: "BLOG_BUILDER_MASTER_KEY", value: "builder-key-secret!"},
		{name: "padded builder key", key: "BLOG_BUILDER_MASTER_KEY", value: master + "="},
		{name: "noncanonical builder key", key: "BLOG_BUILDER_MASTER_KEY", value: master[:len(master)-1] + "R"},
		{name: "wrong size builder key", key: "BLOG_BUILDER_MASTER_KEY", value: base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("m", 31)))},
		{name: "blank artifact path", key: "BLOG_CURRENT_RELEASE_JSON_PATH", value: " \t"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := validEnv()
			if test.remove {
				delete(env, test.key)
			} else {
				env[test.key] = test.value
			}

			_, err := config.Load(func(key string) string { return env[key] })

			require.ErrorContains(t, err, test.key)
			for _, secret := range []string{test.value, env["BLOG_BUNDLE_TOKEN"], env["BLOG_CALLBACK_HMAC_KEY"], env["BLOG_BUILDER_MASTER_KEY"]} {
				if secret != "" {
					require.NotContains(t, err.Error(), secret)
				}
			}
		})
	}
}

func TestValidateRejectsInvalidDirectReleaseConfigWithoutEchoingSecrets(t *testing.T) {
	tests := []struct {
		name, field, secret string
		mutate              func(*config.Config)
	}{
		{name: "bundle token", field: "BLOG_BUNDLE_TOKEN", secret: "bundle-direct-secret", mutate: func(cfg *config.Config) { cfg.Release.BundleToken = []byte("bundle-direct-secret") }},
		{name: "callback key", field: "BLOG_CALLBACK_HMAC_KEY", secret: "callback-direct-secret", mutate: func(cfg *config.Config) { cfg.Release.CallbackHMACKey = []byte("callback-direct-secret") }},
		{name: "builder key", field: "BLOG_BUILDER_MASTER_KEY", secret: "builder-direct-secret", mutate: func(cfg *config.Config) { cfg.Release.BuilderMasterKey = []byte("builder-direct-secret") }},
		{name: "artifact path", field: "BLOG_CURRENT_RELEASE_JSON_PATH", mutate: func(cfg *config.Config) { cfg.Release.CurrentReleaseJSONPath = " \t" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validDirectConfig()
			test.mutate(&cfg)
			err := config.Validate(cfg)
			require.ErrorContains(t, err, test.field)
			if test.secret != "" {
				require.NotContains(t, err.Error(), test.secret)
			}
		})
	}
}

func TestValidateAcceptsCanonicalDevelopmentAndProductionConfig(t *testing.T) {
	development := validDirectConfig()
	require.NoError(t, config.Validate(development))

	development.Session.CookieSecure = true
	require.NoError(t, config.Validate(development))

	production := validDirectConfig()
	production.Environment = "production"
	production.HTTP.AdminOrigin = "https://blog-admin.qiuxs.com"
	production.Session.CookieSecure = true
	production.GFS.BaseURL = "https://gfs.example.com"
	require.NoError(t, config.Validate(production))
}

func TestValidateRejectsInvalidDirectConfigWithoutRevealingValues(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*config.Config)
		wantField  string
		secretText string
	}{
		{name: "empty environment", mutate: func(cfg *config.Config) { cfg.Environment = "" }, wantField: "BLOG_ENV"},
		{name: "unknown environment", mutate: func(cfg *config.Config) { cfg.Environment = "secret-staging" }, wantField: "BLOG_ENV", secretText: "secret-staging"},
		{name: "blank HTTP address", mutate: func(cfg *config.Config) { cfg.HTTP.Addr = " \t" }, wantField: "BLOG_HTTP_ADDR"},
		{name: "missing admin origin", mutate: func(cfg *config.Config) { cfg.HTTP.AdminOrigin = "" }, wantField: "BLOG_ADMIN_ORIGIN"},
		{name: "malformed admin origin", mutate: func(cfg *config.Config) { cfg.HTTP.AdminOrigin = "://origin-secret" }, wantField: "BLOG_ADMIN_ORIGIN", secretText: "origin-secret"},
		{name: "admin origin userinfo", mutate: func(cfg *config.Config) { cfg.HTTP.AdminOrigin = "https://admin-secret@localhost:3000" }, wantField: "BLOG_ADMIN_ORIGIN", secretText: "admin-secret"},
		{name: "noncanonical root slash", mutate: func(cfg *config.Config) { cfg.HTTP.AdminOrigin = "http://localhost:3000/" }, wantField: "BLOG_ADMIN_ORIGIN"},
		{name: "production HTTP origin", mutate: func(cfg *config.Config) {
			cfg.Environment = "production"
			cfg.Session.CookieSecure = true
		}, wantField: "BLOG_ADMIN_ORIGIN"},
		{name: "production insecure cookie", mutate: func(cfg *config.Config) {
			cfg.Environment = "production"
			cfg.HTTP.AdminOrigin = "https://blog-admin.qiuxs.com"
			cfg.Session.CookieSecure = false
		}, wantField: "BLOG_ENV"},
		{name: "blank MySQL DSN", mutate: func(cfg *config.Config) { cfg.MySQL.DSN = " \t" }, wantField: "BLOG_MYSQL_DSN"},
		{name: "blank Redis address", mutate: func(cfg *config.Config) { cfg.Redis.Addr = " \t" }, wantField: "BLOG_REDIS_ADDR"},
		{name: "negative Redis database", mutate: func(cfg *config.Config) { cfg.Redis.DB = -1 }, wantField: "BLOG_REDIS_DB"},
		{name: "zero ID offset", mutate: func(cfg *config.Config) { cfg.IDGen.Offset = 0 }, wantField: "IDGEN_OFFSET"},
		{name: "zero ID step", mutate: func(cfg *config.Config) { cfg.IDGen.Step = 0 }, wantField: "IDGEN_STEP"},
		{name: "offset above step", mutate: func(cfg *config.Config) { cfg.IDGen.Offset = 2 }, wantField: "IDGEN_OFFSET"},
		{name: "invalid cookie name", mutate: func(cfg *config.Config) { cfg.Session.CookieName = "cookie secret" }, wantField: "BLOG_SESSION_COOKIE_NAME", secretText: "cookie secret"},
		{name: "session TTL below minimum", mutate: func(cfg *config.Config) { cfg.Session.TTL = 15*time.Minute - time.Nanosecond }, wantField: "BLOG_SESSION_TTL"},
		{name: "session TTL above maximum", mutate: func(cfg *config.Config) { cfg.Session.TTL = 168*time.Hour + time.Nanosecond }, wantField: "BLOG_SESSION_TTL"},
		{name: "missing GFS base URL", mutate: func(cfg *config.Config) { cfg.GFS.BaseURL = "" }, wantField: "BLOG_GFS_BASE_URL"},
		{name: "malformed GFS base URL", mutate: func(cfg *config.Config) { cfg.GFS.BaseURL = "://gfs-url-secret" }, wantField: "BLOG_GFS_BASE_URL", secretText: "gfs-url-secret"},
		{name: "GFS base URL userinfo", mutate: func(cfg *config.Config) { cfg.GFS.BaseURL = "https://gfs-user-secret@gfs.example.com" }, wantField: "BLOG_GFS_BASE_URL", secretText: "gfs-user-secret"},
		{name: "GFS base URL empty fragment", mutate: func(cfg *config.Config) { cfg.GFS.BaseURL += "#" }, wantField: "BLOG_GFS_BASE_URL"},
		{name: "noncanonical GFS root slash", mutate: func(cfg *config.Config) { cfg.GFS.BaseURL += "/" }, wantField: "BLOG_GFS_BASE_URL"},
		{name: "production HTTP GFS base URL", mutate: func(cfg *config.Config) {
			cfg.Environment = "production"
			cfg.HTTP.AdminOrigin = "https://admin.example.com"
			cfg.Session.CookieSecure = true
		}, wantField: "BLOG_GFS_BASE_URL"},
		{name: "missing GFS app ID", mutate: func(cfg *config.Config) { cfg.GFS.AppID = " \t" }, wantField: "BLOG_GFS_APP_ID"},
		{name: "missing GFS app secret", mutate: func(cfg *config.Config) { cfg.GFS.AppSecret = " \t" }, wantField: "BLOG_GFS_APP_SECRET"},
		{name: "missing GFS public read secret", mutate: func(cfg *config.Config) { cfg.GFS.PublicReadSecret = " \t" }, wantField: "BLOG_GFS_PUBLIC_READ_SECRET"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validDirectConfig()
			test.mutate(&cfg)

			err := config.Validate(cfg)

			require.ErrorContains(t, err, test.wantField)
			if test.secretText != "" {
				require.NotContains(t, err.Error(), test.secretText)
			}
		})
	}
}

func TestLoadRejectsMissingGFSConfigurationWithoutRevealingSecrets(t *testing.T) {
	for _, key := range []string{
		"BLOG_GFS_BASE_URL",
		"BLOG_GFS_APP_ID",
		"BLOG_GFS_APP_SECRET",
		"BLOG_GFS_PUBLIC_READ_SECRET",
	} {
		t.Run(key, func(t *testing.T) {
			env := validEnv()
			delete(env, key)

			_, err := config.Load(func(name string) string { return env[name] })

			require.ErrorContains(t, err, key)
			for _, secret := range []string{"gfs.example.com", "blog-app", "raw-app-secret", "public-read-secret"} {
				require.NotContains(t, err.Error(), secret)
			}
		})
	}
}

func TestLoadNormalizesAndValidatesGFSBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
		wantErr bool
	}{
		{name: "normalizes root slash", baseURL: "http://gfs.example.com/", want: "http://gfs.example.com"},
		{name: "rejects userinfo", baseURL: "https://user@gfs.example.com", wantErr: true},
		{name: "rejects query", baseURL: "https://gfs.example.com?token=value", wantErr: true},
		{name: "rejects forced query", baseURL: "https://gfs.example.com?", wantErr: true},
		{name: "rejects fragment", baseURL: "https://gfs.example.com#part", wantErr: true},
		{name: "rejects empty fragment", baseURL: "https://gfs.example.com#", wantErr: true},
		{name: "rejects non-root path", baseURL: "https://gfs.example.com/api", wantErr: true},
		{name: "rejects unsupported scheme", baseURL: "ftp://gfs.example.com", wantErr: true},
		{name: "rejects missing host", baseURL: "https:", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := validEnv()
			env["BLOG_ENV"] = "development"
			env["BLOG_GFS_BASE_URL"] = test.baseURL

			got, err := config.Load(func(key string) string { return env[key] })

			if test.wantErr {
				require.ErrorContains(t, err, "BLOG_GFS_BASE_URL")
				require.NotContains(t, err.Error(), test.baseURL)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got.GFS.BaseURL)
		})
	}
}

func TestLoadRejectsHTTPGFSBaseURLInProduction(t *testing.T) {
	env := validEnv()
	env["BLOG_GFS_BASE_URL"] = "http://gfs-production-secret.example.com"

	_, err := config.Load(func(key string) string { return env[key] })

	require.ErrorContains(t, err, "BLOG_GFS_BASE_URL")
	require.ErrorContains(t, err, "https")
	require.NotContains(t, err.Error(), "gfs-production-secret")
}

func TestLoadRejectsUnknownEnvironmentAndWhitespaceHTTPAddress(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     string
		wantField string
	}{
		{name: "environment", key: "BLOG_ENV", value: "test", wantField: "BLOG_ENV"},
		{name: "HTTP address", key: "BLOG_HTTP_ADDR", value: " \t", wantField: "BLOG_HTTP_ADDR"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := validEnv()
			env[test.key] = test.value

			_, err := config.Load(func(key string) string { return env[key] })

			require.ErrorContains(t, err, test.wantField)
			require.NotContains(t, err.Error(), test.value)
		})
	}
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

func TestLoadRejectsInvalidSessionCookieName(t *testing.T) {
	for _, cookieName := range []string{
		"session name",
		"session;name",
		"session=name",
		"session/name",
		"session\nname",
		"会话",
	} {
		t.Run(cookieName, func(t *testing.T) {
			env := validEnv()
			env["BLOG_SESSION_COOKIE_NAME"] = cookieName

			_, err := config.Load(func(key string) string { return env[key] })

			require.ErrorContains(t, err, "BLOG_SESSION_COOKIE_NAME")
		})
	}
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
		"BLOG_ENV":                       "production",
		"BLOG_HTTP_ADDR":                 ":9010",
		"BLOG_MYSQL_DSN":                 "blog:secret@tcp(mysql:3306)/qiuxs_blog?parseTime=true&loc=UTC",
		"BLOG_REDIS_ADDR":                "redis:6379",
		"BLOG_REDIS_PASSWORD":            "redis-secret",
		"BLOG_REDIS_DB":                  "2",
		"IDGEN_OFFSET":                   "1",
		"IDGEN_STEP":                     "1",
		"IDGEN_HEAL":                     "false",
		"BLOG_ADMIN_ORIGIN":              "https://blog-admin.qiuxs.com",
		"BLOG_SESSION_COOKIE_NAME":       "qx_blog_session",
		"BLOG_SESSION_TTL":               "24h",
		"BLOG_GFS_BASE_URL":              "https://gfs.example.com/",
		"BLOG_GFS_APP_ID":                "blog-app",
		"BLOG_GFS_APP_SECRET":            "raw-app-secret",
		"BLOG_GFS_PUBLIC_READ_SECRET":    "public-read-secret",
		"BLOG_BUNDLE_TOKEN":              strings.Repeat("b", 32),
		"BLOG_CALLBACK_HMAC_KEY":         strings.Repeat("h", 32),
		"BLOG_BUILDER_MASTER_KEY":        base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))),
		"BLOG_CURRENT_RELEASE_JSON_PATH": "/srv/blog/current/release.json",
	}
}

func validDirectConfig() config.Config {
	return config.Config{
		Environment: "development",
		HTTP: config.HTTPConfig{
			Addr:        ":8080",
			AdminOrigin: "http://localhost:3000",
		},
		MySQL: config.MySQLConfig{DSN: "blog:password@tcp(mysql:3306)/blog"},
		Redis: config.RedisConfig{
			Addr: "redis:6379",
			DB:   0,
		},
		IDGen: config.IDGenConfig{Offset: 1, Step: 1},
		Session: config.SessionConfig{
			CookieName: "qx_blog_session",
			TTL:        24 * time.Hour,
		},
		GFS: config.GFSConfig{
			BaseURL:          "http://gfs.example.com",
			AppID:            "blog-app",
			AppSecret:        "raw-app-secret",
			PublicReadSecret: "public-read-secret",
		},
		Release: config.ReleaseConfig{
			BundleToken:            []byte(strings.Repeat("b", 32)),
			CallbackHMACKey:        []byte(strings.Repeat("h", 32)),
			BuilderMasterKey:       []byte(strings.Repeat("k", 32)),
			CurrentReleaseJSONPath: "/srv/blog/current/release.json",
		},
	}
}
