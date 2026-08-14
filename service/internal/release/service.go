package release

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/settings"
)

type Service struct {
	repository Repository
}

const triggerCompensationTimeout = 5 * time.Second

type BuilderTarget struct {
	BuilderID int64
	Trigger   func(context.Context, int64, int64) (int64, error)
}

type BuilderTargetProvider interface {
	Prepare(context.Context) (BuilderTarget, error)
}

type Orchestrator struct {
	service  *Service
	builder  BuilderTargetProvider
	artifact ArtifactReader
	now      func() time.Time
}

func NewService(repository Repository) (*Service, error) {
	if nilReleaseInterface(repository) {
		return nil, errors.New("release repository is required")
	}
	return &Service{repository: repository}, nil
}

func NewOrchestrator(service *Service, builder BuilderTargetProvider, artifact ArtifactReader, now func() time.Time) (*Orchestrator, error) {
	if service == nil || nilReleaseInterface(service.repository) || nilReleaseInterface(builder) || artifact == nil || now == nil {
		return nil, errors.New("release orchestrator dependencies are required")
	}
	return &Orchestrator{service: service, builder: builder, artifact: artifact, now: now}, nil
}

func (o *Orchestrator) Publish(ctx context.Context, command CreateCommand) (Release, PublishJob, error) {
	_, err := o.operationTime(ctx)
	if err != nil {
		return Release{}, PublishJob{}, err
	}
	if _, err := o.service.Reconcile(ctx, o.artifact); err != nil {
		return Release{}, PublishJob{}, err
	}
	target, err := o.prepareBuilder(ctx)
	if err != nil {
		return Release{}, PublishJob{}, err
	}
	command.BuilderID = target.BuilderID
	created, job, err := o.service.Create(ctx, command)
	if err != nil {
		return Release{}, PublishJob{}, err
	}
	if err := o.trigger(ctx, target, created.ID, job); err != nil {
		return Release{}, PublishJob{}, err
	}
	return created, job, nil
}

func (o *Orchestrator) Retry(ctx context.Context, releaseID int64) (Aggregate, PublishJob, error) {
	_, err := o.operationTime(ctx)
	if err != nil {
		return Aggregate{}, PublishJob{}, err
	}
	target, err := o.prepareBuilder(ctx)
	if err != nil {
		return Aggregate{}, PublishJob{}, err
	}
	aggregate, job, err := o.service.Retry(ctx, releaseID)
	if err != nil {
		return Aggregate{}, PublishJob{}, err
	}
	if err := o.trigger(ctx, target, aggregate.Release.ID, job); err != nil {
		return Aggregate{}, PublishJob{}, err
	}
	return aggregate, job, nil
}

func (o *Orchestrator) Callback(ctx context.Context, event CallbackEvent, verifierDuplicate bool) (PublishJob, bool, error) {
	if err := o.validate(ctx); err != nil {
		return PublishJob{}, false, err
	}
	if err := validateCallbackEvent(event); err != nil {
		return PublishJob{}, false, err
	}
	if verifierDuplicate {
		return PublishJob{}, true, nil
	}
	return o.service.ApplyCallback(ctx, event)
}

func (o *Orchestrator) prepareBuilder(ctx context.Context) (BuilderTarget, error) {
	target, err := o.builder.Prepare(ctx)
	if err != nil {
		return BuilderTarget{}, releaseDependency("load release builder", safeExternalCause(err))
	}
	if target.BuilderID <= 0 || target.Trigger == nil {
		return BuilderTarget{}, releaseDependency("validate release builder", errors.New("release builder is invalid"))
	}
	return target, nil
}

func (o *Orchestrator) trigger(ctx context.Context, target BuilderTarget, releaseID int64, job PublishJob) error {
	var triggerErr error
	if job.BuilderID != target.BuilderID {
		triggerErr = errors.New("release builder identity is invalid")
	} else {
		_, triggerErr = target.Trigger(ctx, releaseID, job.ID)
	}
	if triggerErr == nil {
		return nil
	}
	at := o.now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Before(job.CreatedAt) {
		at = job.CreatedAt.UTC().Truncate(time.Microsecond)
	}
	compensationContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), triggerCompensationTimeout)
	defer cancel()
	failed, _, compensationErr := o.service.repository.FailTriggerLocked(
		compensationContext, job.ID, "Jenkins trigger failed", at,
	)
	if compensationErr != nil {
		return releaseDependency("record Jenkins trigger failure", safeExternalCause(compensationErr))
	}
	if !validTriggerFailure(failed, job, releaseID, at) {
		return releaseDependency("validate Jenkins trigger failure", errors.New("stored trigger failure is invalid"))
	}
	return releaseDependency("trigger Jenkins release", safeExternalCause(triggerErr))
}

func validTriggerFailure(failed, initial PublishJob, releaseID int64, at time.Time) bool {
	return failed.ID == initial.ID && failed.ReleaseID == releaseID && failed.BuilderID == initial.BuilderID &&
		failed.Status == JobFailed && failed.Stage == "trigger" && failed.BuildNumber == nil &&
		failed.ErrorSummary == "Jenkins trigger failed" && !failed.CreatedAt.IsZero() &&
		failed.CreatedAt.Location() == time.UTC && failed.FinishedAt != nil && failed.FinishedAt.Equal(at)
}

func safeExternalCause(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return errors.New("external dependency failed")
	}
}

func (o *Orchestrator) operationTime(ctx context.Context) (time.Time, error) {
	if err := o.validate(ctx); err != nil {
		return time.Time{}, err
	}
	at := o.now()
	if at.IsZero() {
		return time.Time{}, releaseDependency("read release clock", errors.New("release clock is invalid"))
	}
	return at.UTC().Truncate(time.Microsecond), nil
}

func (o *Orchestrator) validate(ctx context.Context) error {
	if o == nil || o.service == nil || nilReleaseInterface(o.service.repository) || nilReleaseInterface(o.builder) || o.artifact == nil || o.now == nil {
		return errors.New("release orchestrator is not configured")
	}
	if nilReleaseInterface(ctx) {
		return releaseDomain("use release orchestrator", ErrConflict)
	}
	return nil
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (Release, PublishJob, error) {
	if err := s.validate(ctx); err != nil {
		return Release{}, PublishJob{}, err
	}
	if err := validateCreateCommand(command); err != nil {
		return Release{}, PublishJob{}, err
	}
	created, job, err := s.repository.CreateLocked(ctx, command)
	if err != nil {
		return Release{}, PublishJob{}, err
	}
	if !validReleaseValue(created) || created.Status != ReleaseQueued || created.CompletedAt != nil ||
		!validInitialJob(job, created.ID, command.BuilderID) {
		return Release{}, PublishJob{}, invalidStoredRelease("validate created release")
	}
	return cloneRelease(created), clonePublishJob(job), nil
}

func (s *Service) Retry(ctx context.Context, releaseID int64) (Aggregate, PublishJob, error) {
	if err := s.validate(ctx); err != nil {
		return Aggregate{}, PublishJob{}, err
	}
	if releaseID <= 0 {
		return Aggregate{}, PublishJob{}, releaseDomain("retry release", ErrNotFound)
	}
	aggregate, created, err := s.repository.CreateRetryLocked(ctx, releaseID)
	if err != nil {
		return Aggregate{}, PublishJob{}, err
	}
	if aggregate.Release.ID != releaseID || !validReleaseValue(aggregate.Release) ||
		aggregate.Release.Status != ReleaseFailed || aggregate.ValidateRetry(created) != nil ||
		!validInitialJob(created, releaseID, created.BuilderID) {
		return Aggregate{}, PublishJob{}, invalidStoredRelease("validate retried release")
	}
	return cloneAggregate(aggregate), clonePublishJob(created), nil
}

func (s *Service) ApplyCallback(ctx context.Context, event CallbackEvent) (PublishJob, bool, error) {
	if err := s.validate(ctx); err != nil {
		return PublishJob{}, false, err
	}
	if err := validateCallbackEvent(event); err != nil {
		return PublishJob{}, false, err
	}
	job, duplicate, err := s.repository.ApplyCallbackLocked(ctx, event)
	if err != nil {
		return PublishJob{}, false, err
	}
	if !validCallbackResult(job, event) {
		return PublishJob{}, false, invalidStoredRelease("validate publish callback")
	}
	return clonePublishJob(job), duplicate, nil
}

func (s *Service) Bundle(ctx context.Context, releaseID int64) ([]byte, string, error) {
	if err := s.validate(ctx); err != nil {
		return nil, "", err
	}
	if releaseID <= 0 {
		return nil, "", releaseDomain("load release bundle", ErrNotFound)
	}
	aggregate, err := s.repository.FindRelease(ctx, releaseID)
	if err != nil {
		return nil, "", err
	}
	latest, err := aggregate.LatestJob()
	if err != nil || aggregate.Release.ID != releaseID || !validReleaseValue(aggregate.Release) {
		return nil, "", invalidStoredRelease("validate release bundle state")
	}
	if latest.Status == JobFailed {
		return nil, "", releaseDomain("load release bundle", ErrNotFound)
	}
	if !downloadableJobStatus(latest.Status) {
		return nil, "", invalidStoredRelease("validate release bundle state")
	}
	bundle, err := s.repository.LoadBundle(ctx, releaseID)
	if err != nil {
		return nil, "", err
	}
	if bundle.ReleaseID != aggregate.Release.ID || bundle.Checksum != aggregate.Release.Checksum ||
		!bundle.GeneratedAt.Equal(aggregate.Release.CreatedAt) || !bundleSiteMatchesRelease(bundle.Site, aggregate.Release.Site) {
		return nil, "", invalidStoredRelease("validate stored release bundle")
	}
	encoded, etag, err := encodeCanonicalBundle(bundle)
	if err != nil {
		return nil, "", invalidStoredRelease("validate stored release bundle")
	}
	return append(make([]byte, 0, len(encoded)), encoded...), etag, nil
}

func (s *Service) Reconcile(ctx context.Context, read ArtifactReader) (bool, error) {
	if err := s.validate(ctx); err != nil {
		return false, err
	}
	if read == nil {
		return false, errors.New("release artifact reader is required")
	}
	reader, err := read()
	if err != nil {
		if !nilReleaseInterface(reader) {
			_ = reader.Close()
		}
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, releaseDependency("open deployed release artifact", err)
	}
	if nilReleaseInterface(reader) {
		return false, errors.New("release artifact reader is not configured")
	}
	artifact, readErr := ReadArtifact(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return false, readErr
	}
	if closeErr != nil {
		return false, releaseDependency("close deployed release artifact", closeErr)
	}
	aggregate, err := s.repository.FindRelease(ctx, artifact.ReleaseID)
	if errors.Is(err, ErrNotFound) {
		return false, releaseDomain("validate deployed release artifact", ErrReconciliationRequired)
	}
	if err != nil {
		return false, err
	}
	if err := aggregate.Validate(); err != nil || !validReleaseValue(aggregate.Release) {
		return false, invalidStoredRelease("validate deployed release state")
	}
	if aggregate.Release.ID != artifact.ReleaseID || aggregate.Release.Checksum != artifact.Checksum || !aggregateHasBuild(aggregate, artifact.BuildNumber) {
		return false, releaseDomain("validate deployed release artifact", ErrReconciliationRequired)
	}
	return s.repository.ReconcileLocked(ctx, artifact)
}

func aggregateHasBuild(aggregate Aggregate, buildNumber int64) bool {
	for _, job := range aggregate.Jobs {
		if job.BuildNumber != nil && *job.BuildNumber == buildNumber {
			return true
		}
	}
	return false
}

func validReleaseValue(value Release) bool {
	if value.ID <= 0 || !releaseChecksumPattern.MatchString(value.Checksum) || value.CreatedAt.IsZero() ||
		value.CreatedAt.Location() != time.UTC || value.CreatedAt.Nanosecond()%1000 != 0 ||
		value.Site.SocialLinks == nil || settings.ValidateReleaseSnapshot(toSettingsSite(value.Site)) != nil {
		return false
	}
	switch value.Status {
	case ReleaseQueued:
		return value.CompletedAt == nil
	case ReleaseSuccess, ReleaseFailed:
		return value.CompletedAt != nil && !value.CompletedAt.IsZero() && value.CompletedAt.Location() == time.UTC && value.CompletedAt.Nanosecond()%1000 == 0
	default:
		return false
	}
}

func validInitialJob(job PublishJob, releaseID, builderID int64) bool {
	return job.ID > 0 && job.ReleaseID == releaseID && builderID > 0 && job.BuilderID == builderID &&
		job.Status == JobPending && job.Stage == "pending" && job.BuildNumber == nil && job.ErrorSummary == "" &&
		!job.CreatedAt.IsZero() && job.CreatedAt.Location() == time.UTC && job.CreatedAt.Nanosecond()%1000 == 0 && job.FinishedAt == nil
}

func validCallbackResult(job PublishJob, event CallbackEvent) bool {
	if job.ID != event.PublishJobID || job.ReleaseID != event.ReleaseID || job.Status != event.Status ||
		job.BuilderID <= 0 || job.Stage != event.Stage || job.BuildNumber == nil || *job.BuildNumber != event.BuildNumber ||
		job.CreatedAt.IsZero() || job.CreatedAt.Location() != time.UTC || job.CreatedAt.Nanosecond()%1000 != 0 {
		return false
	}
	if finalJobStatus(event.Status) {
		return job.FinishedAt != nil && job.FinishedAt.Equal(event.Timestamp.UTC().Truncate(time.Microsecond))
	}
	return job.FinishedAt == nil
}

func downloadableJobStatus(status JobStatus) bool {
	switch status {
	case JobPending, JobQueued, JobBuilding, JobDeploying, JobSuccess:
		return true
	default:
		return false
	}
}

func bundleSiteMatchesRelease(site BundleSite, stored SiteSnapshot) bool {
	return reflect.DeepEqual(SiteSnapshot{
		Name: site.Name, AuthorBio: site.AuthorBio, AboutMarkdown: site.AboutMarkdown,
		FilingName: site.FilingName, FilingNumber: site.FilingNumber,
		SocialLinks: append(make([]SocialLink, 0, len(site.SocialLinks)), site.SocialLinks...),
	}, stored)
}

func invalidStoredRelease(operation string) error {
	return releaseDependency(operation, errors.New("stored release data is invalid"))
}

func (s *Service) validate(ctx context.Context) error {
	if s == nil || nilReleaseInterface(s.repository) {
		return errors.New("release service is not configured")
	}
	if nilReleaseInterface(ctx) {
		return releaseDomain("use release service", ErrConflict)
	}
	return nil
}
