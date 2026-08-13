package media

import (
	"context"
	"time"
)

type Repository interface {
	Create(context.Context, NewMedia, time.Time) (Media, error)
	FindByGFSFileID(context.Context, int64) (Media, error)
	FindActiveByID(context.Context, int64) (Media, error)
	FindActiveByIDs(context.Context, []int64) ([]Media, error)
	FindActiveByPublicKeys(context.Context, []string) ([]Media, error)
	FindActiveByPublicKey(context.Context, string) (Media, error)
}
