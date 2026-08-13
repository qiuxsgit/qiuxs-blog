package config

import (
	"fmt"
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

func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, fmt.Errorf("environment getter is required")
	}

	environment := valueOrDefault(getenv("BLOG_ENV"), defaultEnvironment)
	httpAddr := valueOrDefault(getenv("BLOG_HTTP_ADDR"), defaultHTTPAddr)
	mysqlDSN := getenv("BLOG_MYSQL_DSN")
	if strings.TrimSpace(mysqlDSN) == "" {
		return Config{}, fmt.Errorf("BLOG_MYSQL_DSN is required")
	}

	redisAddr := getenv("BLOG_REDIS_ADDR")
	if strings.TrimSpace(redisAddr) == "" {
		return Config{}, fmt.Errorf("BLOG_REDIS_ADDR is required")
	}
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
	if offset < 1 || step < 1 || offset > step {
		return Config{}, fmt.Errorf("IDGEN_OFFSET and IDGEN_STEP must satisfy 1 <= offset <= step")
	}

	heal, err := parseHeal(getenv("IDGEN_HEAL"))
	if err != nil {
		return Config{}, err
	}

	adminOrigin, err := parseAdminOrigin(getenv("BLOG_ADMIN_ORIGIN"), environment)
	if err != nil {
		return Config{}, err
	}

	cookieName := valueOrDefault(getenv("BLOG_SESSION_COOKIE_NAME"), defaultSessionCookieName)
	ttl, err := parseSessionTTL(getenv("BLOG_SESSION_TTL"))
	if err != nil {
		return Config{}, err
	}

	return Config{
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
	}, nil
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
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("BLOG_ADMIN_ORIGIN is required")
	}

	origin, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("BLOG_ADMIN_ORIGIN must be a valid origin URL")
	}

	scheme := strings.ToLower(origin.Scheme)
	if (scheme != "http" && scheme != "https") || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") || origin.RawPath != "" {
		return "", fmt.Errorf("BLOG_ADMIN_ORIGIN must be an http or https origin without userinfo, query, fragment, or path")
	}
	if environment == "production" && scheme != "https" {
		return "", fmt.Errorf("BLOG_ADMIN_ORIGIN must use https in production")
	}

	return scheme + "://" + origin.Host, nil
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
