package article

import (
	"context"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
)

type Repository interface {
	Create(context.Context, string, time.Time) (Article, error)
	FindByID(context.Context, int64) (Article, error)
	List(context.Context, State) ([]Summary, error)
	SetState(context.Context, int64, State, State, time.Time) error
}

type DraftReader = revision.DraftReader
