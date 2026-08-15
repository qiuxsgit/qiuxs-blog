package platform

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
)

const dependencyTimeout = 5 * time.Second

func OpenMySQL(cfg config.MySQLConfig) (*sql.DB, error) {
	dsn, err := BuildMySQLDSN(cfg)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, errors.New("open mysql connection")
	}

	ConfigureMySQLPool(db)
	ctx, cancel := context.WithTimeout(context.Background(), dependencyTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, errors.New("ping mysql connection")
	}

	return db, nil
}

// BuildMySQLDSN converts the field-based deployment configuration into the
// driver DSN at the process boundary. Passwords never need to be embedded in
// an environment-file DSN string.
func BuildMySQLDSN(cfg config.MySQLConfig) (string, error) {
	if strings.TrimSpace(cfg.Host) == "" || cfg.Port < 1 || cfg.Port > 65535 ||
		strings.TrimSpace(cfg.User) == "" || strings.TrimSpace(cfg.Database) == "" {
		return "", errors.New("mysql connection fields are required")
	}
	params, err := url.ParseQuery(cfg.Args)
	if err != nil {
		return "", errors.New("BLOG_MYSQL_ARGS is invalid")
	}
	mysqlCfg := mysql.Config{
		User:      cfg.User,
		Passwd:    cfg.Password,
		Net:       "tcp",
		Addr:      net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		DBName:    cfg.Database,
		ParseTime: false,
		Params:    make(map[string]string, len(params)),
	}
	for key, values := range params {
		if len(values) != 1 {
			return "", errors.New("BLOG_MYSQL_ARGS must not repeat parameters")
		}
		if key == "parseTime" {
			parsed, parseErr := strconv.ParseBool(values[0])
			if parseErr != nil {
				return "", errors.New("BLOG_MYSQL_ARGS has invalid parseTime")
			}
			mysqlCfg.ParseTime = parsed
			continue
		}
		mysqlCfg.Params[key] = values[0]
	}
	return mysqlCfg.FormatDSN(), nil
}

func ConfigureMySQLPool(db *sql.DB) {
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
}
