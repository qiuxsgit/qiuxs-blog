package dbtable_test

import (
	"context"
	"testing"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/dbtable"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/idgen"
	"github.com/stretchr/testify/require"
)

type recordingCounter struct {
	keys []string
}

func (c *recordingCounter) Increment(_ context.Context, key string) (int64, error) {
	c.keys = append(c.keys, key)
	return 1, nil
}

func (*recordingCounter) Raise(_ context.Context, _ string, floor int64) (int64, error) {
	return floor, nil
}

func TestAllPersistentTablesUseAuditedIDGeneratorNames(t *testing.T) {
	expected := [10]string{
		"articles",
		"article_revisions",
		"tags",
		"article_revision_tags",
		"media",
		"article_revision_media",
		"site_settings",
		"hotlink_settings",
		"referer_allowlist",
		"builder_config",
	}
	require.Equal(t, expected, [10]string{
		dbtable.Articles,
		dbtable.ArticleRevisions,
		dbtable.Tags,
		dbtable.ArticleRevisionTags,
		dbtable.Media,
		dbtable.ArticleRevisionMedia,
		dbtable.SiteSettings,
		dbtable.HotlinkSettings,
		dbtable.RefererAllowlist,
		dbtable.BuilderConfig,
	})
	require.Equal(t, expected, dbtable.All)

	counter := &recordingCounter{}
	generator, err := idgen.New(counter, nil, 1, 1, false)
	require.NoError(t, err)

	for _, table := range dbtable.All {
		id, nextErr := generator.Next(context.Background(), table)
		require.NoError(t, nextErr)
		require.Equal(t, int64(1), id)
	}

	require.Equal(t, []string{
		"idseq:articles",
		"idseq:article_revisions",
		"idseq:tags",
		"idseq:article_revision_tags",
		"idseq:media",
		"idseq:article_revision_media",
		"idseq:site_settings",
		"idseq:hotlink_settings",
		"idseq:referer_allowlist",
		"idseq:builder_config",
	}, counter.keys)
}
