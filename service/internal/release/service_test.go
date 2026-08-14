package release

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReleaseServiceCreateValidatesThenReturnsDetachedCommittedValues(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	checksum := mustPreparedChecksum(t, validPreparedSnapshot(now))
	repository := &repositorySpy{
		createRelease: Release{ID: 7, Status: ReleaseQueued, Site: validPreparedSnapshot(now).Site, Checksum: checksum, CreatedAt: now},
		createJob:     PublishJob{ID: 12, ReleaseID: 7, BuilderID: 9, Status: JobPending, Stage: "pending", CreatedAt: now},
	}
	service, err := NewService(repository)
	require.NoError(t, err)
	command := CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9, RequestedBy: 3}

	releaseValue, job, err := service.Create(context.Background(), command)
	require.NoError(t, err)
	require.Equal(t, command, repository.lastCreate)
	require.Equal(t, int64(7), releaseValue.ID)
	require.Equal(t, int64(12), job.ID)

	releaseValue.Site.SocialLinks = append(releaseValue.Site.SocialLinks, SocialLink{Label: "mutated", URL: "https://example.com"})
	require.Empty(t, repository.createRelease.Site.SocialLinks)

	repository.createRelease.Site.Name = "repository mutation"
	require.Equal(t, "Blog", releaseValue.Site.Name)
	require.Nil(t, job.BuildNumber)
}

func TestReleaseServiceRejectsInvalidCreateAndCorruptCommittedResult(t *testing.T) {
	repository := &repositorySpy{}
	service, err := NewService(repository)
	require.NoError(t, err)
	for name, command := range map[string]CreateCommand{
		"mode":        {Mode: "other", BuilderID: 9},
		"article":     {Mode: PublishArticle, BuilderID: 9},
		"settings id": {Mode: PublishSettings, ArticleID: 1, BuilderID: 9},
		"builder":     {Mode: PublishSettings},
		"requester":   {Mode: PublishSettings, BuilderID: 9, RequestedBy: -1},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := service.Create(context.Background(), command)
			require.ErrorIs(t, err, ErrInvalidSnapshot)
		})
	}
	require.Zero(t, repository.createCalls)

	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repository.createRelease = Release{ID: 7, Status: ReleaseQueued, Checksum: "sha256:" + strings.Repeat("a", 64), CreatedAt: now}
	repository.createJob = PublishJob{ID: 12, ReleaseID: 8, BuilderID: 9, Status: JobPending, Stage: "pending", CreatedAt: now}
	_, _, err = service.Create(context.Background(), CreateCommand{Mode: PublishSettings, BuilderID: 9})
	require.ErrorIs(t, err, ErrDependencyUnavailable)
}

func TestReleaseServiceRetryAndCallbackValidateIdentityAndDetachResults(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	build := int64(44)
	aggregate := validAggregate("sha256:" + strings.Repeat("a", 64))
	failedAt := now
	aggregate.Release.Status = ReleaseFailed
	aggregate.Release.CompletedAt = &failedAt
	aggregate.Release.Site = validPreparedSnapshot(now).Site
	aggregate.Jobs = append([]PublishJob{{ID: 13, ReleaseID: 7, BuilderID: 9, Status: JobPending, Stage: "pending", CreatedAt: now.Add(time.Minute)}}, aggregate.Jobs...)
	repository := &repositorySpy{retryAggregate: aggregate, retryJob: aggregate.Jobs[0]}
	service, err := NewService(repository)
	require.NoError(t, err)

	result, created, err := service.Retry(context.Background(), 7)
	require.NoError(t, err)
	require.NoError(t, result.ValidateRetry(created))
	result.Release.Site.SocialLinks = append(result.Release.Site.SocialLinks, SocialLink{Label: "x", URL: "https://example.com"})
	created.BuildNumber = &build
	require.Empty(t, repository.retryAggregate.Release.Site.SocialLinks)
	require.Nil(t, repository.retryJob.BuildNumber)

	event := CallbackEvent{ReleaseID: 7, PublishJobID: 13, BuildNumber: 44, Stage: "queue", Status: JobQueued, Timestamp: now, Nonce: "nonce"}
	repository.callbackJob = PublishJob{ID: 13, ReleaseID: 7, BuilderID: 9, Status: JobQueued, Stage: "queue", BuildNumber: &build, CreatedAt: now}
	job, duplicate, err := service.ApplyCallback(context.Background(), event)
	require.NoError(t, err)
	require.False(t, duplicate)
	require.Equal(t, event, repository.lastCallback)
	*job.BuildNumber = 99
	require.Equal(t, int64(44), *repository.callbackJob.BuildNumber)

	_, _, err = service.Retry(context.Background(), 0)
	require.ErrorIs(t, err, ErrNotFound)
	_, _, err = service.ApplyCallback(context.Background(), CallbackEvent{})
	require.ErrorIs(t, err, ErrConflict)
}

func TestReleaseServiceRejectsCorruptRetryAndCallbackResults(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	aggregate := validAggregate("sha256:" + strings.Repeat("a", 64))
	aggregate.Jobs = append([]PublishJob{{ID: 13, ReleaseID: 7, BuilderID: 9, Status: JobPending, Stage: "pending", CreatedAt: now.Add(time.Minute)}}, aggregate.Jobs...)
	repository := &repositorySpy{retryAggregate: aggregate, retryJob: aggregate.Jobs[0]}
	service, err := NewService(repository)
	require.NoError(t, err)

	_, _, err = service.Retry(context.Background(), 7)
	require.ErrorIs(t, err, ErrDependencyUnavailable)

	build := int64(44)
	event := CallbackEvent{ReleaseID: 7, PublishJobID: 13, BuildNumber: 44, Stage: "queue", Status: JobQueued, Timestamp: now}
	repository.callbackJob = PublishJob{ID: 13, ReleaseID: 7, BuilderID: 0, Status: JobQueued, Stage: "queue", BuildNumber: &build, CreatedAt: now}
	_, _, err = service.ApplyCallback(context.Background(), event)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
}

func TestReleaseServiceBundleUsesOnlyImmutableRowsAndReturnsCanonicalCopies(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	prepared := validPreparedSnapshot(now)
	bundle, err := assembleBundle(7, now, prepared)
	require.NoError(t, err)
	aggregate := validAggregate(bundle.Checksum)
	aggregate.Release.Site = prepared.Site
	aggregate.Release.CreatedAt = now
	repository := &repositorySpy{aggregate: aggregate, bundle: bundle}
	service, err := NewService(repository)
	require.NoError(t, err)

	first, etag, err := service.Bundle(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, bundle.Checksum, etag)
	require.NotEmpty(t, first)
	first[0] = 'x'
	second, secondETag, err := service.Bundle(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, etag, secondETag)
	require.Equal(t, byte('{'), second[0])
	require.Equal(t, 2, repository.loadCalls)

	// Mutable Stage 2 state is not a Service dependency; only the repository's
	// immutable release Bundle is read.
	require.Equal(t, 2, repository.findCalls)
}

func TestReleaseServiceBundleBlocksLatestFailureAndStoredCorruption(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	prepared := validPreparedSnapshot(now)
	bundle, err := assembleBundle(7, now, prepared)
	require.NoError(t, err)
	aggregate := validAggregate(bundle.Checksum)
	aggregate.Release.Site = prepared.Site
	aggregate.Release.CreatedAt = now
	repository := &repositorySpy{aggregate: aggregate, bundle: bundle}
	service, err := NewService(repository)
	require.NoError(t, err)

	repository.aggregate.Jobs[0].Status = JobFailed
	_, _, err = service.Bundle(context.Background(), 7)
	require.ErrorIs(t, err, ErrNotFound)
	require.Zero(t, repository.loadCalls)

	repository.aggregate.Jobs[0].Status = JobDeploying
	repository.bundle.Checksum = "sha256:" + strings.Repeat("f", 64)
	_, _, err = service.Bundle(context.Background(), 7)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), repository.bundle.Checksum)

	repository.bundle = bundle
	repository.bundle.ReleaseID = 8
	_, _, err = service.Bundle(context.Background(), 7)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
}

func TestReleaseServiceNilSafetyAndRepositoryErrors(t *testing.T) {
	var typedNil *repositorySpy
	_, err := NewService(typedNil)
	require.Error(t, err)
	var service *Service
	_, _, err = service.Create(context.Background(), CreateCommand{Mode: PublishSettings, BuilderID: 9})
	require.Error(t, err)

	repository := &repositorySpy{createErr: releaseDependency("create release", errors.New("database-secret"))}
	service, err = NewService(repository)
	require.NoError(t, err)
	_, _, err = service.Create(context.Background(), CreateCommand{Mode: PublishSettings, BuilderID: 9})
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), "database-secret")
	_, _, err = service.Bundle(nil, 7)
	require.Error(t, err)
}
