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
	FindRelease(context.Context, int64) (Aggregate, error)
	// ListReleases returns aggregates ordered by created_at DESC, id DESC.
	ListReleases(context.Context, ListQuery) ([]Aggregate, error)
	LoadBundle(context.Context, int64) (Bundle, error)
	// CreateRetryLocked atomically returns the new job and its complete updated
	// aggregate; Aggregate.ValidateRetry must succeed for the returned values.
	CreateRetryLocked(context.Context, int64) (Aggregate, PublishJob, error)
	ApplyCallbackLocked(context.Context, CallbackEvent) (PublishJob, bool, error)
	ReconcileLocked(context.Context, Artifact) (bool, error)
}
