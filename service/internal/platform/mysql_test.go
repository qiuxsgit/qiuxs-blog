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
