package dbtable

const (
	Articles             = "articles"
	ArticleRevisions     = "article_revisions"
	Tags                 = "tags"
	ArticleRevisionTags  = "article_revision_tags"
	Media                = "media"
	ArticleRevisionMedia = "article_revision_media"
	SiteSettings         = "site_settings"
	HotlinkSettings      = "hotlink_settings"
	RefererAllowlist     = "referer_allowlist"
	BuilderConfig        = "builder_config"
)

var All = [10]string{
	Articles,
	ArticleRevisions,
	Tags,
	ArticleRevisionTags,
	Media,
	ArticleRevisionMedia,
	SiteSettings,
	HotlinkSettings,
	RefererAllowlist,
	BuilderConfig,
}
