package revision

import (
	"testing"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
	"github.com/stretchr/testify/require"
)

func TestNewInitialDraftReturnsCanonicalCommittedRevision(t *testing.T) {
	at := time.Date(2026, 8, 14, 9, 0, 0, 123000, time.FixedZone("CST", 8*60*60))

	got := NewInitialDraft(11, 21, at)

	require.Equal(t, Draft{
		ID: 21, ArticleID: 11, RevisionNo: 1, LockVersion: 1,
		Status: StatusEditing, Reason: ReasonDraft,
		ContentHash: "9ebca1a33e28c44890c99e46d508488363522c83f17a31056641ad11b11a153f",
		Tags:        []tag.Snapshot{}, Media: []media.Reference{},
		CreatedAt: at.UTC(), UpdatedAt: at.UTC(),
	}, got)
	require.NotNil(t, got.Tags)
	require.NotNil(t, got.Media)
}
