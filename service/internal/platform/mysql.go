package platform

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
)

const dependencyTimeout = 5 * time.Second

func OpenMySQL(cfg config.MySQLConfig) (*sql.DB, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("mysql DSN is required")
	}

	db, err := sql.Open("mysql", cfg.DSN)
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

func ConfigureMySQLPool(db *sql.DB) {
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
}
