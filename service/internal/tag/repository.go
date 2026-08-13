package tag

import (
	"context"
	"time"
)

type Repository interface {
	Create(ctx context.Context, name, slug string, at time.Time) (Tag, error)
	List(ctx context.Context) ([]Tag, error)
	Rename(ctx context.Context, id int64, name string, at time.Time) (Tag, error)
	FindByIDs(ctx context.Context, ids []int64) ([]Tag, error)
}
