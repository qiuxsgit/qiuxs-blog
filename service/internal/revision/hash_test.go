package revision

import (
	"testing"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
	"github.com/stretchr/testify/require"
)

func TestComputeHashUsesCanonicalEmptyPreparedContent(t *testing.T) {
	require.Equal(t, "9ebca1a33e28c44890c99e46d508488363522c83f17a31056641ad11b11a153f", ComputeHash(PreparedContent{}))
	require.Equal(t, ComputeHash(PreparedContent{}), ComputeHash(PreparedContent{Tags: []tag.Snapshot{}}))
}

func TestComputeHashUsesNormalizedScalarsExactMarkdownAndOrderedSnapshots(t *testing.T) {
	cover := &media.Media{ID: 91, PublicKey: firstMediaKey, GFSFileID: 51, OriginalName: "cover.png", State: "active"}
	content := PreparedContent{
		Title: "  Title  ", Summary: "\nSummary ", Cover: cover, ContentMD: "body\n",
		Tags: []tag.Snapshot{
			{TagID: 7, Name: "Go", Slug: "t_go", Position: 0},
			{TagID: 3, Name: "Web", Slug: "t_web", Position: 1},
		},
		Media:       []media.Reference{{MediaID: 91, PublicKey: firstMediaKey, Purpose: "cover", Position: 0}},
		ContentHash: "ignored-existing-hash",
	}

	require.Equal(t, "f01219c74908e464539f6aef66b6fc5c847acf94a72f6ca70d945379a007b328", ComputeHash(content))
	require.Equal(t, ComputeHash(content), ComputeHash(PreparedContent{
		Title: "Title", Summary: "Summary", Cover: cover, ContentMD: "body\n", Tags: content.Tags,
	}))
}

func TestComputeHashIsSensitiveToEveryCanonicalFieldAndTagOrder(t *testing.T) {
	cover := &media.Media{PublicKey: firstMediaKey}
	base := PreparedContent{
		Title: "Title", Summary: "Summary", Cover: cover, ContentMD: "body",
		Tags: []tag.Snapshot{{TagID: 7, Name: "Go", Slug: "t_go"}, {TagID: 3, Name: "Web", Slug: "t_web"}},
	}
	mutations := []PreparedContent{
		{Title: "Other", Summary: base.Summary, Cover: cover, ContentMD: base.ContentMD, Tags: base.Tags},
		{Title: base.Title, Summary: "Other", Cover: cover, ContentMD: base.ContentMD, Tags: base.Tags},
		{Title: base.Title, Summary: base.Summary, ContentMD: base.ContentMD, Tags: base.Tags},
		{Title: base.Title, Summary: base.Summary, Cover: cover, ContentMD: "other", Tags: base.Tags},
		{Title: base.Title, Summary: base.Summary, Cover: cover, ContentMD: base.ContentMD, Tags: []tag.Snapshot{base.Tags[1], base.Tags[0]}},
	}

	for _, mutation := range mutations {
		require.NotEqual(t, ComputeHash(base), ComputeHash(mutation))
	}
}
