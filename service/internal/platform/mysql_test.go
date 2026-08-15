package platform

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/stretchr/testify/require"
)

func TestConfigureMySQLPoolSetsExpectedLimits(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ConfigureMySQLPool(db)

	stats := db.Stats()
	require.Equal(t, 10, stats.MaxOpenConnections)
}

func TestOpenMySQLRejectsEmptyDSN(t *testing.T) {
	db, err := OpenMySQL(config.MySQLConfig{})

	require.Error(t, err)
	require.Nil(t, db)
}

func TestBuildMySQLDSNFromFields(t *testing.T) {
	dsn, err := BuildMySQLDSN(config.MySQLConfig{
		Host: "mysql.internal", Port: 3307, User: "blog", Password: "p@ss:word",
		Database: "qiuxs_blog", Args: "parseTime=true&loc=UTC&charset=utf8mb4",
	})

	require.NoError(t, err)
	require.Contains(t, dsn, "blog:p@ss:word@tcp(mysql.internal:3307)/qiuxs_blog?")
	require.Contains(t, dsn, "parseTime=true")
	require.Contains(t, dsn, "loc=UTC")
	require.Contains(t, dsn, "charset=utf8mb4")
}

func TestBuildMySQLDSNRejectsMalformedArgs(t *testing.T) {
	_, err := BuildMySQLDSN(config.MySQLConfig{
		Host: "mysql", Port: 3306, User: "blog", Database: "blog", Args: "%",
	})

	require.ErrorContains(t, err, "BLOG_MYSQL_ARGS")
}
