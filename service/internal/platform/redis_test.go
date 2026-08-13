package platform

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenRedisUsesSelectedDatabase(t *testing.T) {
	server := miniredis.RunT(t)

	client, err := OpenRedis(config.RedisConfig{Addr: server.Addr(), DB: 3})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.Equal(t, 3, client.Options().DB)
}

func TestOpenRedisRejectsEmptyAddress(t *testing.T) {
	client, err := OpenRedis(config.RedisConfig{})

	require.Error(t, err)
	require.Nil(t, client)
}
