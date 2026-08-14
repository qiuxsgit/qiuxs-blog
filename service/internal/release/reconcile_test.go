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

const validArtifactJSON = `{"releaseId":7,"checksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","buildNumber":44,"deployedAt":"2026-08-14T12:00:00.123456Z"}`

func TestReadArtifactAcceptsOneStrictBoundedObject(t *testing.T) {
	artifact, err := ReadArtifact(strings.NewReader(validArtifactJSON))
	require.NoError(t, err)
	require.Equal(t, Artifact{
		ReleaseID: 7, Checksum: "sha256:" + strings.Repeat("a", 64), BuildNumber: 44,
		DeployedAt: time.Date(2026, 8, 14, 12, 0, 0, 123456000, time.UTC),
	}, artifact)
	boundary := validArtifactJSON + strings.Repeat(" ", maxArtifactBytes-len(validArtifactJSON))
	_, err = ReadArtifact(strings.NewReader(boundary))
	require.NoError(t, err)
}

func TestReadArtifactRejectsMalformedOrAmplifiedInputWithoutLeakingIt(t *testing.T) {
	invalidUTF8 := append([]byte(`{"releaseId":7,"checksum":"sha256:`+strings.Repeat("a", 64)+`","buildNumber":44,"deployedAt":"2026-08-14T12:00:00Z","`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`":"secret"}`)...)
	for name, body := range map[string][]byte{
		"nil reader":        nil,
		"oversized":         []byte(strings.Repeat(" ", 4097)),
		"invalid utf8":      invalidUTF8,
		"array":             []byte(`[]`),
		"duplicate":         []byte(`{"releaseId":7,"releaseId":8,"checksum":"sha256:` + strings.Repeat("a", 64) + `","buildNumber":44,"deployedAt":"2026-08-14T12:00:00Z"}`),
		"unknown":           []byte(`{"releaseId":7,"checksum":"sha256:` + strings.Repeat("a", 64) + `","buildNumber":44,"deployedAt":"2026-08-14T12:00:00Z","secret":"leak-me"}`),
		"missing":           []byte(`{"releaseId":7,"checksum":"sha256:` + strings.Repeat("a", 64) + `","buildNumber":44}`),
		"null":              []byte(`{"releaseId":null,"checksum":"sha256:` + strings.Repeat("a", 64) + `","buildNumber":44,"deployedAt":"2026-08-14T12:00:00Z"}`),
		"trailing document": []byte(validArtifactJSON + `{}`),
		"quoted release":    []byte(strings.Replace(validArtifactJSON, `"releaseId":7`, `"releaseId":"7"`, 1)),
		"quoted build":      []byte(strings.Replace(validArtifactJSON, `"buildNumber":44`, `"buildNumber":"44"`, 1)),
		"exponent release":  []byte(strings.Replace(validArtifactJSON, `"releaseId":7`, `"releaseId":7e0`, 1)),
		"exponent build":    []byte(strings.Replace(validArtifactJSON, `"buildNumber":44`, `"buildNumber":44e0`, 1)),
		"decimal release":   []byte(strings.Replace(validArtifactJSON, `"releaseId":7`, `"releaseId":7.0`, 1)),
		"zero release":      []byte(strings.Replace(validArtifactJSON, `"releaseId":7`, `"releaseId":0`, 1)),
		"decimal build":     []byte(strings.Replace(validArtifactJSON, `"buildNumber":44`, `"buildNumber":4.4`, 1)),
		"zero build":        []byte(strings.Replace(validArtifactJSON, `"buildNumber":44`, `"buildNumber":0`, 1)),
		"leading zero":      []byte(strings.Replace(validArtifactJSON, `"releaseId":7`, `"releaseId":07`, 1)),
		"leading plus":      []byte(strings.Replace(validArtifactJSON, `"releaseId":7`, `"releaseId":+7`, 1)),
		"int64 overflow":    []byte(strings.Replace(validArtifactJSON, `"releaseId":7`, `"releaseId":9223372036854775808`, 1)),
		"build overflow":    []byte(strings.Replace(validArtifactJSON, `"buildNumber":44`, `"buildNumber":9223372036854775808`, 1)),
		"checksum":          []byte(strings.Replace(validArtifactJSON, strings.Repeat("a", 64), "CHECKSUM-SECRET", 1)),
		"timestamp":         []byte(strings.Replace(validArtifactJSON, "2026-08-14T12:00:00.123456Z", "TIMESTAMP-SECRET", 1)),
	} {
		t.Run(name, func(t *testing.T) {
			var reader io.Reader = strings.NewReader(string(body))
			if body == nil {
				reader = nil
			}
			_, err := ReadArtifact(reader)
			require.ErrorIs(t, err, ErrReconciliationRequired)
			for _, secret := range []string{"leak-me", "CHECKSUM-SECRET", "TIMESTAMP-SECRET"} {
				require.NotContains(t, err.Error(), secret)
			}
		})
	}
}

func TestReadArtifactMapsReaderFailureToSanitizedDependencyError(t *testing.T) {
	_, err := ReadArtifact(errorReader{err: errors.New("reader-secret")})
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), "reader-secret")
}

func TestServiceReconcileOwnsReaderLifecycleAndChecksKnownSnapshot(t *testing.T) {
	checksum := "sha256:" + strings.Repeat("a", 64)
	repository := &repositorySpy{aggregate: validAggregate(checksum)}
	service, err := NewService(repository)
	require.NoError(t, err)

	t.Run("missing is not an error", func(t *testing.T) {
		changed, err := service.Reconcile(context.Background(), func() (io.ReadCloser, error) { return nil, fs.ErrNotExist })
		require.NoError(t, err)
		require.False(t, changed)
		require.Zero(t, repository.findCalls)
		require.Zero(t, repository.reconcileCalls)
	})

	t.Run("open error still closes a returned resource", func(t *testing.T) {
		reader := &trackingReadCloser{Reader: strings.NewReader(validArtifactJSON)}
		_, err := service.Reconcile(context.Background(), func() (io.ReadCloser, error) {
			return reader, errors.New("open-secret")
		})
		require.ErrorIs(t, err, ErrDependencyUnavailable)
		require.NotContains(t, err.Error(), "open-secret")
		require.True(t, reader.closed)
		require.Zero(t, repository.findCalls)
	})

	t.Run("reader closes on malformed input", func(t *testing.T) {
		reader := &trackingReadCloser{Reader: strings.NewReader(`{"secret":true}`)}
		_, err := service.Reconcile(context.Background(), func() (io.ReadCloser, error) { return reader, nil })
		require.ErrorIs(t, err, ErrReconciliationRequired)
		require.True(t, reader.closed)
		require.Zero(t, repository.reconcileCalls)
	})

	t.Run("close failure blocks mutation", func(t *testing.T) {
		reader := &trackingReadCloser{Reader: strings.NewReader(validArtifactJSON), closeErr: errors.New("close-secret")}
		_, err := service.Reconcile(context.Background(), func() (io.ReadCloser, error) { return reader, nil })
		require.ErrorIs(t, err, ErrDependencyUnavailable)
		require.NotContains(t, err.Error(), "close-secret")
		require.Zero(t, repository.reconcileCalls)
	})

	t.Run("stored checksum mismatch blocks mutation", func(t *testing.T) {
		repository.aggregate = validAggregate("sha256:" + strings.Repeat("b", 64))
		reader := &trackingReadCloser{Reader: strings.NewReader(validArtifactJSON)}
		_, err := service.Reconcile(context.Background(), func() (io.ReadCloser, error) { return reader, nil })
		require.ErrorIs(t, err, ErrReconciliationRequired)
		require.Zero(t, repository.reconcileCalls)
		require.True(t, reader.closed)
	})

	t.Run("unknown stored release is reconciliation required", func(t *testing.T) {
		repository.findErr = releaseDomain("find release", ErrNotFound)
		reader := &trackingReadCloser{Reader: strings.NewReader(validArtifactJSON)}
		_, err := service.Reconcile(context.Background(), func() (io.ReadCloser, error) { return reader, nil })
		require.ErrorIs(t, err, ErrReconciliationRequired)
		require.Zero(t, repository.reconcileCalls)
		repository.findErr = nil
	})

	t.Run("corrupt stored release blocks mutation", func(t *testing.T) {
		repository.aggregate = validAggregate(checksum)
		repository.aggregate.Release.Site.SocialLinks = nil
		reader := &trackingReadCloser{Reader: strings.NewReader(validArtifactJSON)}
		_, err := service.Reconcile(context.Background(), func() (io.ReadCloser, error) { return reader, nil })
		require.ErrorIs(t, err, ErrDependencyUnavailable)
		require.Zero(t, repository.reconcileCalls)
	})

	t.Run("known artifact delegates exact immutable value", func(t *testing.T) {
		repository.aggregate = validAggregate(checksum)
		repository.reconcileResult = true
		reader := &trackingReadCloser{Reader: strings.NewReader(validArtifactJSON)}
		changed, err := service.Reconcile(context.Background(), func() (io.ReadCloser, error) { return reader, nil })
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, Artifact{ReleaseID: 7, Checksum: checksum, BuildNumber: 44, DeployedAt: time.Date(2026, 8, 14, 12, 0, 0, 123456000, time.UTC)}, repository.lastArtifact)
		require.Equal(t, 1, repository.reconcileCalls)
	})
}

func TestServiceReconcileRejectsNilContextReaderAndTypedNilSafely(t *testing.T) {
	repository := &repositorySpy{aggregate: validAggregate("sha256:" + strings.Repeat("a", 64))}
	service, err := NewService(repository)
	require.NoError(t, err)

	for name, call := range map[string]func() error{
		"nil context": func() error {
			_, err := service.Reconcile(nil, func() (io.ReadCloser, error) { return nil, fs.ErrNotExist })
			return err
		},
		"nil reader function": func() error {
			_, err := service.Reconcile(context.Background(), nil)
			return err
		},
		"typed nil reader": func() error {
			var reader *trackingReadCloser
			_, err := service.Reconcile(context.Background(), func() (io.ReadCloser, error) { return reader, nil })
			return err
		},
	} {
		t.Run(name, func(t *testing.T) { require.Error(t, call()) })
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type trackingReadCloser struct {
	io.Reader
	closed   bool
	closeErr error
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return r.closeErr
}

func validAggregate(checksum string) Aggregate {
	build := int64(44)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	return Aggregate{
		Release: Release{ID: 7, Status: ReleaseQueued, Site: validPreparedSnapshot(now).Site, Checksum: checksum, CreatedAt: now},
		Jobs:    []PublishJob{{ID: 12, ReleaseID: 7, BuilderID: 9, BuilderTarget: testBuilderTarget(), Status: JobDeploying, Stage: "deploy", BuildNumber: &build, CreatedAt: now}},
	}
}

type repositorySpy struct {
	aggregate       Aggregate
	findErr         error
	findCalls       int
	createRelease   Release
	createJob       PublishJob
	createErr       error
	createCalls     int
	lastCreate      CreateCommand
	bundle          Bundle
	bundleErr       error
	loadCalls       int
	retryAggregate  Aggregate
	retryJob        PublishJob
	retryErr        error
	callbackJob     PublishJob
	callbackDup     bool
	callbackErr     error
	lastCallback    CallbackEvent
	reconcileResult bool
	reconcileErr    error
	reconcileCalls  int
	lastArtifact    Artifact
}

func (r *repositorySpy) CreateLocked(_ context.Context, command CreateCommand) (Release, PublishJob, error) {
	r.createCalls++
	r.lastCreate = command
	job := clonePublishJob(r.createJob)
	if job.BuilderTarget == (BuilderTargetSnapshot{}) {
		job.BuilderTarget = testBuilderTarget()
	}
	return cloneRelease(r.createRelease), job, r.createErr
}
func (r *repositorySpy) FindRelease(context.Context, int64) (Aggregate, error) {
	r.findCalls++
	return cloneAggregate(r.aggregate), r.findErr
}
func (r *repositorySpy) ListReleases(context.Context, ListQuery) ([]Aggregate, error) {
	return nil, errors.New("not configured")
}
func (r *repositorySpy) LoadBundleSnapshot(context.Context, int64) (Aggregate, Bundle, error) {
	r.loadCalls++
	return cloneAggregate(r.aggregate), r.bundle, r.bundleErr
}
func (r *repositorySpy) CreateRetryLocked(context.Context, int64, int64, BuilderTargetSnapshot) (Aggregate, PublishJob, error) {
	aggregate := cloneAggregate(r.retryAggregate)
	for index := range aggregate.Jobs {
		if aggregate.Jobs[index].BuilderTarget == (BuilderTargetSnapshot{}) {
			aggregate.Jobs[index].BuilderTarget = testBuilderTarget()
		}
	}
	job := clonePublishJob(r.retryJob)
	if job.BuilderTarget == (BuilderTargetSnapshot{}) {
		job.BuilderTarget = testBuilderTarget()
	}
	return aggregate, job, r.retryErr
}
func (r *repositorySpy) ApplyCallbackLocked(_ context.Context, event CallbackEvent) (PublishJob, bool, error) {
	r.lastCallback = event
	job := clonePublishJob(r.callbackJob)
	if job.BuilderTarget == (BuilderTargetSnapshot{}) {
		job.BuilderTarget = testBuilderTarget()
	}
	return job, r.callbackDup, r.callbackErr
}
func (r *repositorySpy) FailTriggerLocked(context.Context, int64, string, time.Time) (PublishJob, bool, error) {
	return PublishJob{}, false, errors.New("not configured")
}
func (r *repositorySpy) ReconcileLocked(_ context.Context, artifact Artifact) (bool, error) {
	r.reconcileCalls++
	r.lastArtifact = artifact
	return r.reconcileResult, r.reconcileErr
}
