package auth

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func openSessionRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return server, client
}

func TestRedisSessionStoreSetUsesNamespacedDigestKeyAndMinimalJSON(t *testing.T) {
	server, client := openSessionRedis(t)
	store := NewRedisSessionStore(client)
	ctx := context.Background()
	const digest = "ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0"
	session := Session{AdminID: 42, Username: "qiuxs", ExpiresAt: time.Now().Add(2 * time.Hour).UTC().Round(0)}

	err := store.Set(ctx, digest, session, 2*time.Minute)

	require.NoError(t, err)
	const key = "qiuxs-blog:session:ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0"
	rawJSON, err := server.Get(key)
	require.NoError(t, err)
	var stored map[string]any
	require.NoError(t, json.Unmarshal([]byte(rawJSON), &stored))
	require.Equal(t, map[string]any{
		"adminId":   float64(42),
		"username":  "qiuxs",
		"expiresAt": session.ExpiresAt.Format(time.RFC3339Nano),
	}, stored)
	require.GreaterOrEqual(t, server.TTL(key), time.Minute+59*time.Second)
	require.LessOrEqual(t, server.TTL(key), 2*time.Minute)
}

func TestRedisSessionStoreGetMapsMissingExpiredAndMalformedValuesToNotFound(t *testing.T) {
	server, client := openSessionRedis(t)
	store := NewRedisSessionStore(client)
	ctx := context.Background()
	const digest = "ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0"
	const key = "qiuxs-blog:session:ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0"

	_, err := store.Get(ctx, digest)
	require.ErrorIs(t, err, ErrSessionNotFound)

	require.NoError(t, client.Set(ctx, key, `{"adminId":42,"username":"qiuxs","expiresAt":"2020-01-01T00:00:00Z"}`, time.Minute).Err())
	_, err = store.Get(ctx, digest)
	require.ErrorIs(t, err, ErrSessionNotFound)

	require.NoError(t, client.Set(ctx, key, `{not-json`, time.Minute).Err())
	_, err = store.Get(ctx, digest)
	require.ErrorIs(t, err, ErrSessionNotFound)

	require.NoError(t, client.Set(ctx, key, `{"adminId":0,"username":"qiuxs","expiresAt":"2099-01-01T00:00:00Z"}`, time.Minute).Err())
	_, err = store.Get(ctx, digest)
	require.ErrorIs(t, err, ErrSessionNotFound)

	require.NoError(t, client.Set(ctx, key, `{"adminId":42,"username":"qiuxs","expiresAt":"2099-01-01T00:00:00Z"}`, time.Second).Err())
	server.FastForward(time.Second)
	_, err = store.Get(ctx, digest)
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestRedisSessionStoreGetReturnsStoredSession(t *testing.T) {
	_, client := openSessionRedis(t)
	store := NewRedisSessionStore(client)
	session := Session{AdminID: 42, Username: "qiuxs", ExpiresAt: time.Now().Add(time.Hour).UTC().Round(0)}

	require.NoError(t, store.Set(context.Background(), "ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0", session, time.Minute))
	got, err := store.Get(context.Background(), "ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0")

	require.NoError(t, err)
	require.Equal(t, session, got)
}

func TestRedisSessionStoreDeleteIsIdempotent(t *testing.T) {
	server, client := openSessionRedis(t)
	store := NewRedisSessionStore(client)
	ctx := context.Background()
	const digest = "ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0"
	const key = "qiuxs-blog:session:ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0"
	require.NoError(t, client.Set(ctx, key, "value", time.Minute).Err())

	require.NoError(t, store.Delete(ctx, digest))
	require.False(t, server.Exists(key))
	require.NoError(t, store.Delete(ctx, digest))
}

func TestRedisSessionStoreSetRejectsInvalidSession(t *testing.T) {
	_, client := openSessionRedis(t)
	store := NewRedisSessionStore(client)

	for _, session := range []Session{
		{},
		{AdminID: -1, Username: "qiuxs", ExpiresAt: time.Now().Add(time.Hour)},
		{AdminID: 1, ExpiresAt: time.Now().Add(time.Hour)},
		{AdminID: 1, Username: "qiuxs", ExpiresAt: time.Now()},
	} {
		err := store.Set(context.Background(), "ea866a757e4c38babfa8127cbe9a409d3e1f93a00ff1488ff735fcf917afffd0", session, time.Minute)
		require.Error(t, err)
		require.False(t, errors.Is(err, ErrSessionNotFound))
	}
}
