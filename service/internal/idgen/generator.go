package idgen

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"

	"github.com/go-sql-driver/mysql"
)

var tableNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var primaryKeyErrorPattern = regexp.MustCompile("(?i)for key ['`](?:[^'`.]+\\.)?PRIMARY['`]")

const maxHealingRetries = 5

type Counter interface {
	Increment(ctx context.Context, key string) (int64, error)
	Raise(ctx context.Context, key string, floor int64) (int64, error)
}

type Generator struct {
	counter Counter
	db      *sql.DB
	offset  int64
	step    int64
	heal    bool
}

func New(counter Counter, db *sql.DB, offset, step int64, heal bool) (*Generator, error) {
	if offset < 1 || step < 1 || offset > step {
		return nil, fmt.Errorf("id generation lane must satisfy 1 <= offset <= step")
	}
	if counter == nil {
		return nil, fmt.Errorf("ID counter is required")
	}
	if heal && db == nil {
		return nil, fmt.Errorf("database is required when ID healing is enabled")
	}

	return &Generator{
		counter: counter,
		db:      db,
		offset:  offset,
		step:    step,
		heal:    heal,
	}, nil
}

func (g *Generator) HealEnabled() bool {
	return g.heal
}

func (g *Generator) Next(ctx context.Context, table string) (int64, error) {
	key, err := counterKey(table)
	if err != nil {
		return 0, err
	}

	raw, err := g.counter.Increment(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("increment id counter for table %q: %w", table, err)
	}
	if raw <= 0 {
		return 0, fmt.Errorf("id counter for table %q returned non-positive value %d", table, raw)
	}
	if raw-1 > (math.MaxInt64-g.offset)/g.step {
		return 0, fmt.Errorf("id generation overflow for table %q", table)
	}

	return g.offset + (raw-1)*g.step, nil
}

func (g *Generator) Insert(ctx context.Context, table string, insert func(id int64) error) error {
	if _, err := counterKey(table); err != nil {
		return err
	}

	for retry := 0; retry <= maxHealingRetries; retry++ {
		id, err := g.Next(ctx, table)
		if err != nil {
			return fmt.Errorf("generate ID for insert into %q: %w", table, err)
		}

		err = insert(id)
		if err == nil {
			return nil
		}
		if !IsPKConflict(err) {
			return fmt.Errorf("insert into %q: %w", table, err)
		}
		if !g.heal {
			return fmt.Errorf("primary key conflict inserting into %q and ID healing is disabled: %w", table, err)
		}
		if retry == maxHealingRetries {
			return fmt.Errorf("primary key conflict inserting into %q after %d healing retries: %w", table, maxHealingRetries, err)
		}
		if healErr := g.Heal(ctx, table); healErr != nil {
			return fmt.Errorf("heal ID counter for %q after primary key conflict: %w", table, healErr)
		}
	}

	return fmt.Errorf("unreachable ID insert retry state")
}

func (g *Generator) Heal(ctx context.Context, table string) error {
	key, err := counterKey(table)
	if err != nil {
		return err
	}
	if !g.heal {
		return fmt.Errorf("ID healing is disabled")
	}
	if g.db == nil {
		return fmt.Errorf("database is required when ID healing is enabled")
	}

	var maxID sql.NullInt64
	query := fmt.Sprintf("SELECT MAX(id) FROM %s", table)
	if err := g.db.QueryRowContext(ctx, query).Scan(&maxID); err != nil {
		return fmt.Errorf("query maximum ID from %q: %w", table, err)
	}

	max := int64(0)
	if maxID.Valid {
		max = maxID.Int64
	}
	next, err := nextInLane(max, g.offset, g.step)
	if err != nil {
		return err
	}
	rawFloor := (next - g.offset) / g.step
	if rawFloor == 0 {
		return nil
	}
	if _, err := g.counter.Raise(ctx, key, rawFloor); err != nil {
		return fmt.Errorf("raise ID counter for %q: %w", table, err)
	}
	return nil
}

func IsPKConflict(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) &&
		mysqlErr.Number == 1062 &&
		primaryKeyErrorPattern.MatchString(mysqlErr.Message)
}

func nextInLane(max, offset, step int64) (int64, error) {
	if max < offset {
		return offset, nil
	}

	remainder := (max - offset) % step
	advance := step - remainder
	if max > math.MaxInt64-advance {
		return 0, fmt.Errorf("cannot allocate an ID above maximum signed int64")
	}
	return max + advance, nil
}

func counterKey(table string) (string, error) {
	if !tableNamePattern.MatchString(table) {
		return "", fmt.Errorf("invalid table name %q", table)
	}
	return "idseq:" + table, nil
}
