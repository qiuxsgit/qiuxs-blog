package release

import "context"

type SnapshotSource interface {
	LoadCurrentSite(context.Context) (SiteSnapshot, error)
	LoadPublishedArticles(context.Context, int64) ([]ArticleSnapshot, error)
	FreezeForPublish(context.Context, int64) (ArticleSnapshot, error)
	RemoveFromPublish(context.Context, int64) error
}

type Repository interface {
	CreateLocked(context.Context, CreateCommand) (Release, PublishJob, error)
	FindRelease(context.Context, int64) (Release, error)
	ListReleases(context.Context) ([]Release, error)
	LoadBundle(context.Context, int64) (Bundle, error)
	CreateRetryLocked(context.Context, int64) (PublishJob, error)
	ApplyCallbackLocked(context.Context, CallbackEvent) (PublishJob, bool, error)
	ReconcileLocked(context.Context, Artifact) (bool, error)
}
