package idgen

import (
	"context"
	"math"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	return server, client
}

func TestRedisNextUsesOnlyValidatedTableKey(t *testing.T) {
	server, client := openTestRedis(t)
	generator, err := New(NewRedisCounter(client), nil, 1, 1, false)
	require.NoError(t, err)

	id, err := generator.Next(context.Background(), "admins")

	require.NoError(t, err)
	assert.Equal(t, int64(1), id)
	assert.Equal(t, []string{"idseq:admins"}, server.Keys())
}

func TestRedisRaiseOnlyMovesCounterForward(t *testing.T) {
	_, client := openTestRedis(t)
	counter := NewRedisCounter(client)
	ctx := context.Background()
	require.NoError(t, client.Set(ctx, "idseq:admins", "3", 0).Err())

	raised, err := counter.Raise(ctx, "idseq:admins", 10)
	require.NoError(t, err)
	kept, err := counter.Raise(ctx, "idseq:admins", 7)

	require.NoError(t, err)
	assert.Equal(t, int64(10), raised)
	assert.Equal(t, int64(10), kept)
	assert.Equal(t, "10", client.Get(ctx, "idseq:admins").Val())
}

func TestRedisRaisePreservesFullSignedInt64Range(t *testing.T) {
	_, client := openTestRedis(t)
	counter := NewRedisCounter(client)
	ctx := context.Background()
	require.NoError(t, client.Set(ctx, "idseq:admins", "9007199254740993", 0).Err())

	raised, err := counter.Raise(ctx, "idseq:admins", math.MaxInt64)
	require.NoError(t, err)
	kept, err := counter.Raise(ctx, "idseq:admins", 9007199254740994)

	require.NoError(t, err)
	assert.Equal(t, int64(math.MaxInt64), raised)
	assert.Equal(t, int64(math.MaxInt64), kept)
	assert.Equal(t, "9223372036854775807", client.Get(ctx, "idseq:admins").Val())
}

func TestRedisRaiseRejectsNonPositiveFloor(t *testing.T) {
	_, client := openTestRedis(t)
	counter := NewRedisCounter(client)

	for _, floor := range []int64{0, -1} {
		got, err := counter.Raise(context.Background(), "idseq:admins", floor)

		assert.Zero(t, got)
		assert.ErrorContains(t, err, "positive")
	}
}

func TestRedisRaiseConcurrentCallsKeepHighestFloor(t *testing.T) {
	_, client := openTestRedis(t)
	counter := NewRedisCounter(client)
	ctx := context.Background()

	const workers = 100
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for floor := int64(1); floor <= workers; floor++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := counter.Raise(ctx, "idseq:admins", floor)
			if err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, "100", client.Get(ctx, "idseq:admins").Val())
}

func TestRedisNextAfterRaiseUsesFollowingRawCounter(t *testing.T) {
	_, client := openTestRedis(t)
	counter := NewRedisCounter(client)
	generator, err := New(counter, nil, 1, 1, false)
	require.NoError(t, err)

	got, err := counter.Raise(context.Background(), "idseq:admins", 10)
	require.NoError(t, err)
	assert.Equal(t, int64(10), got)
	id, err := generator.Next(context.Background(), "admins")

	require.NoError(t, err)
	assert.Equal(t, int64(11), id)
}

func TestConcurrentNextReturnsDistinctPositiveIDs(t *testing.T) {
	_, client := openTestRedis(t)
	generator, err := New(NewRedisCounter(client), nil, 1, 1, false)
	require.NoError(t, err)

	const workers = 100
	ids := make(chan int64, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, nextErr := generator.Next(context.Background(), "admins")
			if nextErr != nil {
				errs <- nextErr
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	seen := make(map[int64]struct{}, workers)
	for id := range ids {
		assert.Positive(t, id)
		_, duplicate := seen[id]
		assert.False(t, duplicate, "duplicate ID %d", id)
		seen[id] = struct{}{}
	}
	assert.Len(t, seen, workers)
}
