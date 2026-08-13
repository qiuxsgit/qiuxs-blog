package config

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEnvironment       = "development"
	defaultHTTPAddr          = ":8080"
	defaultSessionCookieName = "qx_blog_session"
	defaultSessionTTL        = 24 * time.Hour
	minimumSessionTTL        = 15 * time.Minute
	maximumSessionTTL        = 7 * 24 * time.Hour
)

type Config struct {
	Environment string
	HTTP        HTTPConfig
	MySQL       MySQLConfig
	Redis       RedisConfig
	IDGen       IDGenConfig
	Session     SessionConfig
	GFS         GFSConfig
	Release     ReleaseConfig
}

type HTTPConfig struct{ Addr, AdminOrigin string }
type MySQLConfig struct{ DSN string }
type RedisConfig struct {
	Addr, Password string
	DB             int
}
type IDGenConfig struct {
	Offset, Step int64
	Heal         bool
}
type SessionConfig struct {
	CookieName   string
	CookieSecure bool
	TTL          time.Duration
}
type GFSConfig struct {
	BaseURL          string
	AppID            string
	AppSecret        string
	PublicReadSecret string
}
type ReleaseConfig struct {
	BundleToken            []byte
	CallbackHMACKey        []byte
	BuilderMasterKey       []byte
	CurrentReleaseJSONPath string
}

func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, fmt.Errorf("environment getter is required")
	}

	environment := valueOrDefault(getenv("BLOG_ENV"), defaultEnvironment)
	httpAddr := valueOrDefault(getenv("BLOG_HTTP_ADDR"), defaultHTTPAddr)
	mysqlDSN := getenv("BLOG_MYSQL_DSN")

	redisAddr := getenv("BLOG_REDIS_ADDR")
	redisDB, err := parseRedisDB(getenv("BLOG_REDIS_DB"))
	if err != nil {
		return Config{}, err
	}

	offset, err := parseIDSetting("IDGEN_OFFSET", getenv("IDGEN_OFFSET"), 1)
	if err != nil {
		return Config{}, err
	}
	step, err := parseIDSetting("IDGEN_STEP", getenv("IDGEN_STEP"), 1)
	if err != nil {
		return Config{}, err
	}
	heal, err := parseHeal(getenv("IDGEN_HEAL"))
	if err != nil {
		return Config{}, err
	}

	adminOrigin, err := parseAdminOrigin(getenv("BLOG_ADMIN_ORIGIN"), environment)
	if err != nil {
		return Config{}, err
	}
	gfsBaseURL, err := parseHTTPOrigin(getenv("BLOG_GFS_BASE_URL"), "BLOG_GFS_BASE_URL", environment)
	if err != nil {
		return Config{}, err
	}

	cookieName := valueOrDefault(getenv("BLOG_SESSION_COOKIE_NAME"), defaultSessionCookieName)
	ttl, err := parseSessionTTL(getenv("BLOG_SESSION_TTL"))
	if err != nil {
		return Config{}, err
	}
	bundleToken, err := parseOpaqueReleaseSecret("BLOG_BUNDLE_TOKEN", getenv("BLOG_BUNDLE_TOKEN"))
	if err != nil {
		return Config{}, err
	}
	callbackKey, err := parseOpaqueReleaseSecret("BLOG_CALLBACK_HMAC_KEY", getenv("BLOG_CALLBACK_HMAC_KEY"))
	if err != nil {
		return Config{}, err
	}
	builderKey, err := parseBuilderMasterKey(getenv("BLOG_BUILDER_MASTER_KEY"))
	if err != nil {
		return Config{}, err
	}
	currentReleasePath := valueOrDefault(getenv("BLOG_CURRENT_RELEASE_JSON_PATH"), "/web/deploy/blog-site/current/release.json")

	cfg := Config{
		Environment: environment,
		HTTP: HTTPConfig{
			Addr:        httpAddr,
			AdminOrigin: adminOrigin,
		},
		MySQL: MySQLConfig{DSN: mysqlDSN},
		Redis: RedisConfig{
			Addr:     redisAddr,
			Password: getenv("BLOG_REDIS_PASSWORD"),
			DB:       redisDB,
		},
		IDGen: IDGenConfig{
			Offset: offset,
			Step:   step,
			Heal:   heal,
		},
		Session: SessionConfig{
			CookieName:   cookieName,
			CookieSecure: environment == "production",
			TTL:          ttl,
		},
		GFS: GFSConfig{
			BaseURL:          gfsBaseURL,
			AppID:            getenv("BLOG_GFS_APP_ID"),
			AppSecret:        getenv("BLOG_GFS_APP_SECRET"),
			PublicReadSecret: getenv("BLOG_GFS_PUBLIC_READ_SECRET"),
		},
		Release: ReleaseConfig{
			BundleToken:            bundleToken,
			CallbackHMACKey:        callbackKey,
			BuilderMasterKey:       builderKey,
			CurrentReleaseJSONPath: currentReleasePath,
		},
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks a fully parsed Config without normalizing it. In particular,
// HTTP.AdminOrigin and GFS.BaseURL must already equal the canonical origins
// Load emits: lower-case scheme and host with no trailing slash, path, query,
// fragment, or userinfo.
// Returned errors name environment variables but never include their values.
func Validate(cfg Config) error {
	if cfg.Environment != "development" && cfg.Environment != "production" {
		return fmt.Errorf("BLOG_ENV must be development or production")
	}
	if strings.TrimSpace(cfg.HTTP.Addr) == "" {
		return fmt.Errorf("BLOG_HTTP_ADDR is required")
	}
	canonicalOrigin, err := parseAdminOrigin(cfg.HTTP.AdminOrigin, cfg.Environment)
	if err != nil {
		return err
	}
	if canonicalOrigin != cfg.HTTP.AdminOrigin {
		return fmt.Errorf("BLOG_ADMIN_ORIGIN must be canonical without a trailing slash")
	}
	if strings.TrimSpace(cfg.MySQL.DSN) == "" {
		return fmt.Errorf("BLOG_MYSQL_DSN is required")
	}
	if strings.TrimSpace(cfg.Redis.Addr) == "" {
		return fmt.Errorf("BLOG_REDIS_ADDR is required")
	}
	if cfg.Redis.DB < 0 {
		return fmt.Errorf("BLOG_REDIS_DB must be a non-negative integer")
	}
	if cfg.IDGen.Offset < 1 || cfg.IDGen.Step < 1 || cfg.IDGen.Offset > cfg.IDGen.Step {
		return fmt.Errorf("IDGEN_OFFSET and IDGEN_STEP must satisfy 1 <= offset <= step")
	}
	if cfg.Environment == "production" && !cfg.Session.CookieSecure {
		return fmt.Errorf("BLOG_ENV production requires a secure session cookie")
	}
	if err := ValidateSessionCookieName(cfg.Session.CookieName); err != nil {
		return err
	}
	if cfg.Session.TTL < minimumSessionTTL || cfg.Session.TTL > maximumSessionTTL {
		return fmt.Errorf("BLOG_SESSION_TTL must be between 15m and 168h")
	}
	canonicalGFSBaseURL, err := parseHTTPOrigin(cfg.GFS.BaseURL, "BLOG_GFS_BASE_URL", cfg.Environment)
	if err != nil {
		return err
	}
	if canonicalGFSBaseURL != cfg.GFS.BaseURL {
		return fmt.Errorf("BLOG_GFS_BASE_URL must be canonical without a trailing slash")
	}
	if strings.TrimSpace(cfg.GFS.AppID) == "" {
		return fmt.Errorf("BLOG_GFS_APP_ID is required")
	}
	if strings.TrimSpace(cfg.GFS.AppSecret) == "" {
		return fmt.Errorf("BLOG_GFS_APP_SECRET is required")
	}
	if strings.TrimSpace(cfg.GFS.PublicReadSecret) == "" {
		return fmt.Errorf("BLOG_GFS_PUBLIC_READ_SECRET is required")
	}
	if err := validateOpaqueReleaseSecret("BLOG_BUNDLE_TOKEN", cfg.Release.BundleToken); err != nil {
		return err
	}
	if err := validateOpaqueReleaseSecret("BLOG_CALLBACK_HMAC_KEY", cfg.Release.CallbackHMACKey); err != nil {
		return err
	}
	if len(cfg.Release.BuilderMasterKey) != 32 {
		return fmt.Errorf("BLOG_BUILDER_MASTER_KEY must decode to 32 bytes")
	}
	if strings.TrimSpace(cfg.Release.CurrentReleaseJSONPath) == "" {
		return fmt.Errorf("BLOG_CURRENT_RELEASE_JSON_PATH is required")
	}
	return nil
}

func parseOpaqueReleaseSecret(field, value string) ([]byte, error) {
	secret := []byte(value)
	if err := validateOpaqueReleaseSecret(field, secret); err != nil {
		return nil, err
	}
	return append([]byte(nil), secret...), nil
}

func validateOpaqueReleaseSecret(field string, secret []byte) error {
	if len(secret) < 32 || len(secret) > 128 || strings.TrimSpace(string(secret)) == "" {
		return fmt.Errorf("%s must be between 32 and 128 bytes", field)
	}
	return nil
}

func parseBuilderMasterKey(encoded string) ([]byte, error) {
	decoded, err := base64.RawStdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) != 32 || base64.RawStdEncoding.EncodeToString(decoded) != encoded {
		return nil, fmt.Errorf("BLOG_BUILDER_MASTER_KEY must be canonical RawStd base64 encoding 32 bytes")
	}
	return append([]byte(nil), decoded...), nil
}

// ValidateSessionCookieName requires an ASCII RFC token suitable for an HTTP
// Cookie name. The returned error never includes the candidate value.
func ValidateSessionCookieName(name string) error {
	if err := (&http.Cookie{Name: name}).Valid(); err != nil {
		return fmt.Errorf("BLOG_SESSION_COOKIE_NAME must be a valid HTTP cookie name")
	}
	return nil
}

func valueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func parseRedisDB(value string) (int, error) {
	if value == "" {
		return 0, nil
	}

	db, err := strconv.Atoi(value)
	if err != nil || db < 0 {
		return 0, fmt.Errorf("BLOG_REDIS_DB must be a non-negative integer")
	}
	return db, nil
}

func parseIDSetting(key, value string, defaultValue int64) (int64, error) {
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a signed 64-bit integer", key)
	}
	return parsed, nil
}

func parseHeal(value string) (bool, error) {
	if value == "" {
		return false, nil
	}

	heal, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("IDGEN_HEAL must be a boolean")
	}
	return heal, nil
}

func parseAdminOrigin(value, environment string) (string, error) {
	return parseHTTPOrigin(value, "BLOG_ADMIN_ORIGIN", environment)
}

func parseHTTPOrigin(value, field, environment string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if strings.Contains(value, "#") {
		return "", fmt.Errorf("%s must be an http or https origin without userinfo, query, fragment, or path", field)
	}

	origin, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%s must be a valid origin URL", field)
	}

	scheme := strings.ToLower(origin.Scheme)
	if (scheme != "http" && scheme != "https") || origin.Host == "" || origin.Hostname() == "" || origin.User != nil || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") || origin.RawPath != "" || origin.Opaque != "" {
		return "", fmt.Errorf("%s must be an http or https origin without userinfo, query, fragment, or path", field)
	}
	if environment == "production" && scheme != "https" {
		return "", fmt.Errorf("%s must use https in production", field)
	}

	return scheme + "://" + strings.ToLower(origin.Host), nil
}

func parseSessionTTL(value string) (time.Duration, error) {
	if value == "" {
		return defaultSessionTTL, nil
	}

	ttl, err := time.ParseDuration(value)
	if err != nil || ttl < minimumSessionTTL || ttl > maximumSessionTTL {
		return 0, fmt.Errorf("BLOG_SESSION_TTL must be between 15m and 168h")
	}
	return ttl, nil
}
