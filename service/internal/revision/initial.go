package revision

import (
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
)

func NewInitialDraft(articleID, revisionID int64, at time.Time) Draft {
	at = at.UTC()
	return Draft{
		ID:          revisionID,
		ArticleID:   articleID,
		RevisionNo:  1,
		LockVersion: 1,
		Status:      StatusEditing,
		Reason:      ReasonDraft,
		ContentHash: ComputeHash(PreparedContent{}),
		Tags:        []tag.Snapshot{},
		Media:       []media.Reference{},
		CreatedAt:   at,
		UpdatedAt:   at,
	}
}
