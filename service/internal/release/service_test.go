package release

import (
	"context"
	"errors"
	"io"
	"io/fs"
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

func TestOrchestratorPublishReconcilesLoadsBuilderCommitsThenTriggersExactAttempt(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	order := make([]string, 0, 4)
	repository := newOrchestratorRepository(t, now, &order)
	service, err := NewService(repository)
	require.NoError(t, err)
	provider := &orchestratorBuilderProvider{order: &order, target: BuilderTarget{
		BuilderID: 9,
		Trigger: func(_ context.Context, releaseID, publishJobID int64) (int64, error) {
			order = append(order, "trigger")
			require.Equal(t, int64(7), releaseID)
			require.Equal(t, int64(12), publishJobID)
			return 55, nil
		},
	}}
	orchestrator, err := NewOrchestrator(service, provider, func() (io.ReadCloser, error) {
		order = append(order, "reconcile")
		return nil, fs.ErrNotExist
	}, func() time.Time { return now })
	require.NoError(t, err)

	created, job, err := orchestrator.Publish(context.Background(), CreateCommand{Mode: PublishSettings, RequestedBy: 3})
	require.NoError(t, err)
	require.Equal(t, int64(7), created.ID)
	require.Equal(t, int64(12), job.ID)
	require.Equal(t, CreateCommand{Mode: PublishSettings, BuilderID: 9, RequestedBy: 3}, repository.lastCreate)
	require.Equal(t, []string{"reconcile", "builder", "create", "trigger"}, order)
	require.Zero(t, repository.failCalls)
}

func TestOrchestratorTriggerFailureCompensatesExactJobAfterRequestCancellation(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 123000, time.UTC)
	repository := newOrchestratorRepository(t, now, nil)
	service, err := NewService(repository)
	require.NoError(t, err)
	requestContext, cancel := context.WithCancel(context.Background())
	provider := &orchestratorBuilderProvider{target: BuilderTarget{
		BuilderID: 9,
		Trigger: func(context.Context, int64, int64) (int64, error) {
			cancel()
			return 0, errors.New("private Jenkins URL and token")
		},
	}}
	orchestrator, err := NewOrchestrator(service, provider, missingArtifactReader, func() time.Time { return now })
	require.NoError(t, err)

	_, _, err = orchestrator.Publish(requestContext, CreateCommand{Mode: PublishSettings})
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), "private")
	require.Equal(t, 1, repository.failCalls)
	require.Equal(t, int64(12), repository.failedJobID)
	require.Equal(t, "Jenkins trigger failed", repository.failedSummary)
	require.Equal(t, now, repository.failedAt)
	require.NoError(t, repository.failedContextErr)
	require.True(t, repository.failedHasDeadline)
}

func TestOrchestratorSamplesTriggerFailureAfterAttemptAndClampsDatabaseClockSkew(t *testing.T) {
	start := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	for name, postTrigger := range map[string]time.Time{
		"later application clock": start.Add(2 * time.Minute),
		"database clock ahead":    start.Add(30 * time.Second),
	} {
		t.Run(name, func(t *testing.T) {
			repository := newOrchestratorRepository(t, start, nil)
			repository.createJob.CreatedAt = start.Add(time.Minute)
			service, err := NewService(repository)
			require.NoError(t, err)
			provider := &orchestratorBuilderProvider{target: BuilderTarget{BuilderID: 9, Trigger: func(context.Context, int64, int64) (int64, error) {
				return 0, errors.New("trigger failed")
			}}}
			clockCalls := 0
			clock := func() time.Time {
				clockCalls++
				if clockCalls == 1 {
					return start
				}
				return postTrigger
			}
			orchestrator, err := NewOrchestrator(service, provider, missingArtifactReader, clock)
			require.NoError(t, err)

			_, _, err = orchestrator.Publish(context.Background(), CreateCommand{Mode: PublishSettings})
			require.ErrorIs(t, err, ErrDependencyUnavailable)
			expected := postTrigger
			if expected.Before(repository.createJob.CreatedAt) {
				expected = repository.createJob.CreatedAt
			}
			require.Equal(t, 2, clockCalls)
			require.Equal(t, expected, repository.failedAt)
			require.False(t, repository.failedAt.Before(repository.createJob.CreatedAt))
		})
	}
}

func TestOrchestratorRetryUsesNewJobWithoutChangingImmutableRelease(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repository := newOrchestratorRepository(t, now, nil)
	failedAt := now.Add(-time.Minute)
	repository.retryAggregate = validAggregate(repository.createRelease.Checksum)
	repository.retryAggregate.Release = repository.createRelease
	repository.retryAggregate.Release.Status = ReleaseFailed
	repository.retryAggregate.Release.CompletedAt = &failedAt
	repository.retryAggregate.Jobs = []PublishJob{
		{ID: 13, ReleaseID: 7, BuilderID: 9, Status: JobPending, Stage: "pending", CreatedAt: now},
		{ID: 12, ReleaseID: 7, BuilderID: 9, Status: JobFailed, Stage: "deploy", BuildNumber: int64Pointer(44), CreatedAt: now.Add(-time.Minute), FinishedAt: &failedAt},
	}
	repository.retryJob = repository.retryAggregate.Jobs[0]
	service, err := NewService(repository)
	require.NoError(t, err)
	provider := &orchestratorBuilderProvider{target: BuilderTarget{BuilderID: 9, Trigger: func(_ context.Context, releaseID, jobID int64) (int64, error) {
		require.Equal(t, int64(7), releaseID)
		require.Equal(t, int64(13), jobID)
		return 56, nil
	}}}
	orchestrator, err := NewOrchestrator(service, provider, missingArtifactReader, func() time.Time { return now })
	require.NoError(t, err)

	aggregate, job, err := orchestrator.Retry(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int64(13), job.ID)
	require.Equal(t, repository.createRelease.Checksum, aggregate.Release.Checksum)
	require.Equal(t, 1, repository.retryCalls)
	require.Zero(t, repository.failCalls)
}

func TestOrchestratorSkipsRepositoryForVerifierDuplicateAndDispatchesFirstExactly(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repository := newOrchestratorRepository(t, now, nil)
	build := int64(44)
	repository.callbackJob = PublishJob{ID: 12, ReleaseID: 7, BuilderID: 9, Status: JobQueued, Stage: "queue", BuildNumber: &build, CreatedAt: now}
	service, err := NewService(repository)
	require.NoError(t, err)
	orchestrator, err := NewOrchestrator(service, &orchestratorBuilderProvider{}, missingArtifactReader, func() time.Time { return now })
	require.NoError(t, err)
	event := CallbackEvent{ReleaseID: 7, PublishJobID: 12, BuildNumber: 44, Stage: "queue", Status: JobQueued, Timestamp: now, Nonce: "nonce_1234567890"}

	job, duplicate, err := orchestrator.Callback(context.Background(), event, true)
	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, PublishJob{}, job)
	require.Zero(t, repository.callbackCalls)

	job, duplicate, err = orchestrator.Callback(context.Background(), event, false)
	require.NoError(t, err)
	require.False(t, duplicate)
	require.Equal(t, int64(12), job.ID)
	require.Equal(t, event, repository.lastCallback)
	require.Equal(t, 1, repository.callbackCalls)

	second := event
	second.Nonce = "different_nonce_1"
	repository.callbackDup = true
	_, duplicate, err = orchestrator.Callback(context.Background(), second, false)
	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, 2, repository.callbackCalls)
}

func TestOrchestratorFailurePrecedenceAndNilSafety(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	repository := newOrchestratorRepository(t, now, nil)
	service, err := NewService(repository)
	require.NoError(t, err)
	provider := &orchestratorBuilderProvider{target: BuilderTarget{BuilderID: 9, Trigger: func(context.Context, int64, int64) (int64, error) {
		return 0, errors.New("trigger-secret")
	}}}

	orchestrator, err := NewOrchestrator(service, provider, func() (io.ReadCloser, error) {
		return nil, errors.New("artifact-secret")
	}, func() time.Time { return now })
	require.NoError(t, err)
	_, _, err = orchestrator.Publish(context.Background(), CreateCommand{Mode: PublishSettings})
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.Zero(t, provider.prepareCalls)
	require.Zero(t, repository.createCalls)
	require.NotContains(t, err.Error(), "artifact-secret")

	orchestrator.artifact = missingArtifactReader
	provider.err = errors.New("builder-token-secret")
	_, _, err = orchestrator.Publish(context.Background(), CreateCommand{Mode: PublishSettings})
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.Zero(t, repository.createCalls)
	require.NotContains(t, err.Error(), "builder-token-secret")

	provider.err = nil
	repository.createErr = releaseDependency("create release", errors.New("database-secret"))
	_, _, err = orchestrator.Publish(context.Background(), CreateCommand{Mode: PublishSettings})
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.Zero(t, repository.failCalls)

	repository.createErr = nil
	repository.failErr = releaseDependency("fail trigger", errors.New("compensation-secret"))
	_, _, err = orchestrator.Publish(context.Background(), CreateCommand{Mode: PublishSettings})
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), "trigger-secret")
	require.NotContains(t, err.Error(), "compensation-secret")

	for _, construct := range []func() (*Orchestrator, error){
		func() (*Orchestrator, error) { return NewOrchestrator(nil, provider, missingArtifactReader, time.Now) },
		func() (*Orchestrator, error) { return NewOrchestrator(service, nil, missingArtifactReader, time.Now) },
		func() (*Orchestrator, error) { return NewOrchestrator(service, provider, nil, time.Now) },
		func() (*Orchestrator, error) { return NewOrchestrator(service, provider, missingArtifactReader, nil) },
	} {
		var got *Orchestrator
		require.NotPanics(t, func() { got, err = construct() })
		require.Nil(t, got)
		require.Error(t, err)
	}
	var nilOrchestrator *Orchestrator
	require.NotPanics(t, func() { _, _, err = nilOrchestrator.Publish(context.Background(), CreateCommand{}) })
	require.Error(t, err)
}

func missingArtifactReader() (io.ReadCloser, error) { return nil, fs.ErrNotExist }

type orchestratorBuilderProvider struct {
	target       BuilderTarget
	err          error
	prepareCalls int
	order        *[]string
}

func (p *orchestratorBuilderProvider) Prepare(context.Context) (BuilderTarget, error) {
	p.prepareCalls++
	if p.order != nil {
		*p.order = append(*p.order, "builder")
	}
	return p.target, p.err
}

type orchestratorRepository struct {
	*repositorySpy
	order             *[]string
	retryCalls        int
	callbackCalls     int
	failCalls         int
	failedJobID       int64
	failedSummary     string
	failedAt          time.Time
	failedContextErr  error
	failedHasDeadline bool
	failErr           error
}

func newOrchestratorRepository(t *testing.T, now time.Time, order *[]string) *orchestratorRepository {
	t.Helper()
	prepared := validPreparedSnapshot(now)
	checksum := mustPreparedChecksum(t, prepared)
	return &orchestratorRepository{repositorySpy: &repositorySpy{
		createRelease: Release{ID: 7, Status: ReleaseQueued, Site: prepared.Site, Checksum: checksum, CreatedAt: now},
		createJob:     PublishJob{ID: 12, ReleaseID: 7, BuilderID: 9, Status: JobPending, Stage: "pending", CreatedAt: now},
	}, order: order}
}

func (r *orchestratorRepository) CreateLocked(ctx context.Context, command CreateCommand) (Release, PublishJob, error) {
	if r.order != nil {
		*r.order = append(*r.order, "create")
	}
	return r.repositorySpy.CreateLocked(ctx, command)
}

func (r *orchestratorRepository) CreateRetryLocked(context.Context, int64) (Aggregate, PublishJob, error) {
	r.retryCalls++
	return cloneAggregate(r.retryAggregate), clonePublishJob(r.retryJob), r.retryErr
}

func (r *orchestratorRepository) ApplyCallbackLocked(ctx context.Context, event CallbackEvent) (PublishJob, bool, error) {
	r.callbackCalls++
	return r.repositorySpy.ApplyCallbackLocked(ctx, event)
}

func (r *orchestratorRepository) FailTriggerLocked(ctx context.Context, jobID int64, summary string, at time.Time) (PublishJob, bool, error) {
	r.failCalls++
	r.failedJobID, r.failedSummary, r.failedAt = jobID, summary, at
	r.failedContextErr = ctx.Err()
	_, r.failedHasDeadline = ctx.Deadline()
	if r.failErr != nil {
		return PublishJob{}, false, r.failErr
	}
	return PublishJob{ID: jobID, ReleaseID: 7, BuilderID: 9, Status: JobFailed, Stage: "trigger", ErrorSummary: summary, CreatedAt: r.createJob.CreatedAt, FinishedAt: &at}, false, nil
}

func int64Pointer(value int64) *int64 { return &value }
