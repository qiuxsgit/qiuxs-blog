package release

import (
	"context"
	"database/sql"
	"time"
)

type SnapshotExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type SnapshotSource interface {
	PrepareSnapshot(context.Context, SnapshotExecutor, SnapshotRequest) (PreparedSnapshot, error)
}

type Repository interface {
	CreateLocked(context.Context, CreateCommand) (Release, PublishJob, error)
	FindRelease(context.Context, int64) (Aggregate, error)
	// ListReleases returns aggregates ordered by created_at DESC, id DESC.
	ListReleases(context.Context, ListQuery) ([]Aggregate, error)
	// LoadBundleSnapshot returns the eligibility aggregate and immutable bundle
	// from one repeatable-read transaction.
	LoadBundleSnapshot(context.Context, int64) (Aggregate, Bundle, error)
	// CreateRetryLocked atomically returns the new job and its complete updated
	// aggregate; Aggregate.ValidateRetry must succeed for the returned values.
	CreateRetryLocked(context.Context, int64, int64, BuilderTargetSnapshot) (Aggregate, PublishJob, error)
	ApplyCallbackLocked(context.Context, CallbackEvent) (PublishJob, bool, error)
	FailTriggerLocked(context.Context, int64, string, time.Time) (PublishJob, bool, error)
	ReconcileLocked(context.Context, Artifact) (bool, error)
}
