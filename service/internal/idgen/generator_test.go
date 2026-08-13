package idgen

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCounter struct {
	mu             sync.Mutex
	values         map[string]int64
	incrementErr   error
	incrementCalls []string
	raiseCalls     []raiseCall
}

type raiseCall struct {
	key   string
	floor int64
}

func newFakeCounter() *fakeCounter {
	return &fakeCounter{values: make(map[string]int64)}
}

func (c *fakeCounter) Increment(_ context.Context, key string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.incrementCalls = append(c.incrementCalls, key)
	if c.incrementErr != nil {
		return 0, c.incrementErr
	}
	c.values[key]++
	return c.values[key], nil
}

func (c *fakeCounter) Raise(_ context.Context, key string, floor int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.raiseCalls = append(c.raiseCalls, raiseCall{key: key, floor: floor})
	if c.values[key] < floor {
		c.values[key] = floor
	}
	return c.values[key], nil
}

func TestNextAllocatesConfiguredLane(t *testing.T) {
	tests := []struct {
		name   string
		offset int64
		step   int64
		want   []int64
	}{
		{name: "default lane", offset: 1, step: 1, want: []int64{1, 2, 3}},
		{name: "second lane of three", offset: 2, step: 3, want: []int64{2, 5, 8}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter := newFakeCounter()
			generator, err := New(counter, nil, tt.offset, tt.step, false)
			require.NoError(t, err)

			got := make([]int64, 0, len(tt.want))
			for range tt.want {
				id, nextErr := generator.Next(context.Background(), "admins")
				require.NoError(t, nextErr)
				assert.Positive(t, id)
				got = append(got, id)
			}

			assert.Equal(t, tt.want, got)
			assert.Equal(t, []string{"idseq:admins", "idseq:admins", "idseq:admins"}, counter.incrementCalls)
		})
	}
}

func TestNewRejectsInvalidLane(t *testing.T) {
	tests := []struct {
		name   string
		offset int64
		step   int64
	}{
		{name: "zero offset", offset: 0, step: 1},
		{name: "negative offset", offset: -1, step: 1},
		{name: "zero step", offset: 1, step: 0},
		{name: "negative step", offset: 1, step: -1},
		{name: "offset exceeds step", offset: 3, step: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator, err := New(newFakeCounter(), nil, tt.offset, tt.step, false)

			assert.Nil(t, generator)
			assert.Error(t, err)
		})
	}
}

func TestNewRejectsNilCounter(t *testing.T) {
	generator, err := New(nil, nil, 1, 1, false)

	assert.Nil(t, generator)
	assert.ErrorContains(t, err, "counter")
}

func TestNewRejectsTypedNilCounter(t *testing.T) {
	var counter *fakeCounter

	generator, err := New(counter, nil, 1, 1, false)

	assert.Nil(t, generator)
	assert.ErrorContains(t, err, "counter")
}

func TestGeneratorMethodsRejectNilAndZeroValueWithoutPanic(t *testing.T) {
	var nilGenerator *Generator
	var typedNilCounter *fakeCounter
	zeroGenerator := &Generator{}

	for _, test := range []struct {
		name      string
		generator *Generator
	}{
		{name: "nil receiver", generator: nilGenerator},
		{name: "zero value", generator: zeroGenerator},
		{name: "invalid lane", generator: &Generator{counter: newFakeCounter()}},
		{name: "typed nil counter", generator: &Generator{counter: typedNilCounter, offset: 1, step: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, call := range []struct {
				name string
				fn   func() error
			}{
				{name: "next", fn: func() error {
					_, callErr := test.generator.Next(context.Background(), "admins")
					return callErr
				}},
				{name: "insert", fn: func() error {
					return test.generator.Insert(context.Background(), "admins", func(int64) error {
						t.Fatal("insert callback must not run for an unconfigured generator")
						return nil
					})
				}},
				{name: "heal", fn: func() error {
					return test.generator.Heal(context.Background(), "admins")
				}},
			} {
				t.Run(call.name, func(t *testing.T) {
					var err error
					require.NotPanics(t, func() { err = call.fn() })
					assert.EqualError(t, err, "id generator is not configured")
				})
			}
		})
	}
}

func TestHealEnabledIsSafeForNilAndZeroValue(t *testing.T) {
	var nilGenerator *Generator

	assert.NotPanics(t, func() { assert.False(t, nilGenerator.HealEnabled()) })
	assert.False(t, (&Generator{}).HealEnabled())
}

func TestNextRejectsInvalidTableBeforeCounterUse(t *testing.T) {
	tests := []string{
		"",
		"Admins",
		"1admins",
		"admin-users",
		"admins.id",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	for _, table := range tests {
		t.Run(table, func(t *testing.T) {
			counter := newFakeCounter()
			generator, err := New(counter, nil, 1, 1, false)
			require.NoError(t, err)

			id, err := generator.Next(context.Background(), table)

			assert.Zero(t, id)
			assert.Error(t, err)
			assert.Empty(t, counter.incrementCalls)
		})
	}
}

func TestNextWrapsCounterErrorAndReturnsNoID(t *testing.T) {
	counterErr := errors.New("redis unavailable")
	counter := newFakeCounter()
	counter.incrementErr = counterErr
	generator, err := New(counter, nil, 1, 1, false)
	require.NoError(t, err)

	id, err := generator.Next(context.Background(), "admins")

	assert.Zero(t, id)
	assert.ErrorIs(t, err, counterErr)
	assert.ErrorContains(t, err, "increment id counter")
}

func TestNextRejectsNonPositiveRawCounter(t *testing.T) {
	for _, raw := range []int64{0, -1} {
		t.Run(fmt.Sprintf("raw_%d", raw), func(t *testing.T) {
			counter := newFakeCounter()
			counter.values["idseq:admins"] = raw - 1
			generator, err := New(counter, nil, 1, 1, false)
			require.NoError(t, err)

			id, err := generator.Next(context.Background(), "admins")

			assert.Zero(t, id)
			assert.ErrorContains(t, err, "non-positive")
		})
	}
}

func TestNextRejectsSignedArithmeticOverflow(t *testing.T) {
	counter := newFakeCounter()
	counter.values["idseq:admins"] = math.MaxInt64 - 1
	generator, err := New(counter, nil, 1, 2, false)
	require.NoError(t, err)

	var id int64
	id, err = generator.Next(context.Background(), "admins")

	assert.Zero(t, id)
	assert.ErrorContains(t, err, "overflow")
}

func TestIsPKConflictRecognizesOnlyPrimaryDuplicateErrors(t *testing.T) {
	primary := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry '1' for key 'PRIMARY'"}
	qualifiedPrimary := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry '1' for key 'admins.PRIMARY'"}
	backtickPrimary := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry '1' for key `PRIMARY`"}
	business := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'qiuxs' for key 'uk_admins_username'"}
	primarySubstring := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'x' for key 'uk_PRIMARY_backup'"}
	wrongNumber := &mysql.MySQLError{Number: 1061, Message: "Duplicate entry '1' for key 'PRIMARY'"}

	assert.True(t, IsPKConflict(primary))
	assert.True(t, IsPKConflict(fmt.Errorf("repository insert: %w", primary)))
	assert.True(t, IsPKConflict(qualifiedPrimary))
	assert.True(t, IsPKConflict(backtickPrimary))
	assert.False(t, IsPKConflict(business))
	assert.False(t, IsPKConflict(primarySubstring))
	assert.False(t, IsPKConflict(wrongNumber))
	assert.False(t, IsPKConflict(errors.New("Duplicate entry '1' for key 'PRIMARY'")))
}

func TestNewRejectsNilDatabaseWhenHealingEnabled(t *testing.T) {
	generator, err := New(newFakeCounter(), nil, 1, 1, true)

	assert.Nil(t, generator)
	assert.ErrorContains(t, err, "database")
}

func TestGeneratorReportsWhetherHealingIsEnabled(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	disabled, err := New(newFakeCounter(), nil, 1, 1, false)
	require.NoError(t, err)
	enabled, err := New(newFakeCounter(), db, 1, 1, true)
	require.NoError(t, err)

	assert.False(t, disabled.HealEnabled())
	assert.True(t, enabled.HealEnabled())
}

func TestInsertReturnsWrappedPrimaryConflictWhenHealingDisabled(t *testing.T) {
	primary := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry '1' for key 'PRIMARY'"}
	generator, err := New(newFakeCounter(), nil, 1, 1, false)
	require.NoError(t, err)
	attempts := 0

	err = generator.Insert(context.Background(), "admins", func(_ int64) error {
		attempts++
		return primary
	})

	assert.ErrorIs(t, err, primary)
	assert.ErrorContains(t, err, "healing is disabled")
	assert.Equal(t, 1, attempts)
}

func TestInsertPassesThroughBusinessUniqueConflictWithoutHealing(t *testing.T) {
	business := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'qiuxs' for key 'uk_admins_username'"}
	generator, err := New(newFakeCounter(), nil, 1, 1, false)
	require.NoError(t, err)
	attempts := 0

	err = generator.Insert(context.Background(), "admins", func(_ int64) error {
		attempts++
		return business
	})

	assert.ErrorIs(t, err, business)
	assert.NotContains(t, err.Error(), "healing is disabled")
	assert.Equal(t, 1, attempts)
}

func TestInsertHealsWrappedPrimaryConflictIntoConfiguredLane(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery(regexp.QuoteMeta("SELECT MAX(id) FROM admins")).
		WillReturnRows(sqlmock.NewRows([]string{"MAX(id)"}).AddRow(int64(100)))

	counter := newFakeCounter()
	generator, err := New(counter, db, 2, 3, true)
	require.NoError(t, err)
	primary := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry '2' for key 'PRIMARY'"}
	var ids []int64

	err = generator.Insert(context.Background(), "admins", func(id int64) error {
		ids = append(ids, id)
		if len(ids) == 1 {
			return fmt.Errorf("insert admin: %w", primary)
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []int64{2, 101}, ids)
	assert.Equal(t, []raiseCall{{key: "idseq:admins", floor: 33}}, counter.raiseCalls)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHealEmptyTableLeavesNextIDAtConfiguredOffset(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery(regexp.QuoteMeta("SELECT MAX(id) FROM admins")).
		WillReturnRows(sqlmock.NewRows([]string{"MAX(id)"}).AddRow(nil))

	counter := newFakeCounter()
	generator, err := New(counter, db, 2, 3, true)
	require.NoError(t, err)

	require.NoError(t, generator.Heal(context.Background(), "admins"))
	id, err := generator.Next(context.Background(), "admins")

	require.NoError(t, err)
	assert.Equal(t, int64(2), id)
	assert.Empty(t, counter.raiseCalls)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestInsertStopsAfterFiveHealingRetries(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	for range 5 {
		mock.ExpectQuery(regexp.QuoteMeta("SELECT MAX(id) FROM admins")).
			WillReturnRows(sqlmock.NewRows([]string{"MAX(id)"}).AddRow(int64(10)))
	}

	counter := newFakeCounter()
	generator, err := New(counter, db, 1, 1, true)
	require.NoError(t, err)
	primary := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry for key 'PRIMARY'"}
	attempts := 0

	err = generator.Insert(context.Background(), "admins", func(_ int64) error {
		attempts++
		return primary
	})

	assert.ErrorIs(t, err, primary)
	assert.ErrorContains(t, err, "after 5 healing retries")
	assert.Equal(t, 6, attempts)
	assert.Len(t, counter.raiseCalls, 5)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHealRejectsInvalidTableBeforeSQL(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	generator, err := New(newFakeCounter(), db, 1, 1, true)
	require.NoError(t, err)

	err = generator.Heal(context.Background(), "admins; DROP TABLE admins")

	assert.ErrorContains(t, err, "invalid table")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNextInLaneReturnsSmallestGreaterConfiguredID(t *testing.T) {
	tests := []struct {
		name         string
		max          int64
		offset, step int64
		want         int64
	}{
		{name: "above lane member", max: 2, offset: 2, step: 3, want: 5},
		{name: "above gap", max: 100, offset: 2, step: 3, want: 101},
		{name: "first lane member", max: 1, offset: 2, step: 3, want: 2},
		{name: "empty table", max: 0, offset: 2, step: 3, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nextInLane(tt.max, tt.offset, tt.step)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNextInLaneReturnsClearErrorAtMaxInt64(t *testing.T) {
	got, err := nextInLane(math.MaxInt64, 1, 1)

	assert.Zero(t, got)
	assert.EqualError(t, err, "cannot allocate an ID above maximum signed int64")
}

func TestNextInLaneHandlesSignedBoundaryWithMultiStepLane(t *testing.T) {
	got, err := nextInLane(math.MaxInt64-1, 1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(math.MaxInt64), got)

	got, err = nextInLane(math.MaxInt64-2, 2, 3)
	assert.Zero(t, got)
	assert.EqualError(t, err, "cannot allocate an ID above maximum signed int64")
}
