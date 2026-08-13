package revision

import (
	"context"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
)

type Repository interface {
	GetDraft(context.Context, int64) (Draft, error)
	SaveDraft(context.Context, int64, int64, PreparedContent, time.Time) (Draft, error)
	CreateVersion(context.Context, int64, int64, int64, time.Time) (Version, Draft, error)
	ListVersions(context.Context, int64) ([]Version, error)
	RestoreVersion(context.Context, int64, int64, int64, int64, time.Time) (Draft, error)
}

type TagResolver interface {
	Snapshots(context.Context, []int64) ([]tag.Snapshot, error)
}

type MediaResolver interface {
	ResolveReferences(context.Context, *int64, []string) (*media.Media, []media.Reference, error)
}
