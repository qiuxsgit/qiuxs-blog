package flow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/builder"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/httpapi"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/release"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAuthenticatedCallbackReplayRetriesAfterDatabaseApplyFailure(t *testing.T) {
	flow := newCallbackReplayFlow(t, true)

	first := flow.send(t)
	require.Equal(t, http.StatusServiceUnavailable, first.Code, first.Body.String())
	second := flow.send(t)
	require.Equal(t, http.StatusNoContent, second.Code, second.Body.String())
	require.Equal(t, 2, flow.repository.calls())
}

func TestAuthenticatedConcurrentCallbackDuplicatesAllReachDatabaseIdempotency(t *testing.T) {
	flow := newCallbackReplayFlow(t, false)
	const attempts = 8
	responses := make(chan int, attempts)
	var group sync.WaitGroup
	for index := 0; index < attempts; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			responses <- flow.send(t).Code
		}()
	}
	group.Wait()
	close(responses)
	for status := range responses {
		require.Equal(t, http.StatusNoContent, status)
	}
	require.Equal(t, attempts, flow.repository.calls())
}

type callbackReplayFlow struct {
	router     http.Handler
	repository *callbackReplayRepository
	raw        []byte
	signature  string
}

func newCallbackReplayFlow(t *testing.T, failFirst bool) callbackReplayFlow {
	t.Helper()
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	key := bytes.Repeat([]byte{'h'}, 32)
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	verifier, err := builder.NewCallbackVerifier(key, client, func() time.Time { return now })
	require.NoError(t, err)
	repository := &callbackReplayRepository{failFirst: failFirst, now: now}
	service, err := release.NewService(repository)
	require.NoError(t, err)
	orchestrator, err := release.NewOrchestrator(service, callbackReplayBuilder{}, func() (io.ReadCloser, error) {
		return nil, fs.ErrNotExist
	}, func() time.Time { return now })
	require.NoError(t, err)
	router := gin.New()
	router.POST("/callback", httpapi.VerifyJenkinsCallback(verifier), func(c *gin.Context) {
		payload, duplicate, ok := httpapi.JenkinsCallbackFrom(c)
		if !ok {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		if _, _, callbackErr := orchestrator.Callback(c.Request.Context(), payload.Event(), duplicate); callbackErr != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.Status(http.StatusNoContent)
	})
	payload := builder.CallbackPayload{
		ReleaseID: 7, PublishJobID: 12, BuildNumber: 44, Stage: "queue", Status: release.JobQueued,
		Timestamp: now, Nonce: "nonce_1234567890",
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	canonical := append([]byte(strconv.FormatInt(now.Unix(), 10)+"\n"+payload.Nonce+"\n"), raw...)
	return callbackReplayFlow{
		router: router, repository: repository, raw: raw,
		signature: "sha256=" + platform.ComputeHMAC(key, canonical),
	}
}

func (f callbackReplayFlow) send(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/callback", bytes.NewReader(f.raw))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Jenkins-Signature", f.signature)
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	return response
}

type callbackReplayBuilder struct{}

func (callbackReplayBuilder) Prepare(context.Context) (release.BuilderTarget, error) {
	return release.BuilderTarget{BuilderID: 9, Trigger: func(context.Context, int64, int64) (int64, error) { return 1, nil }}, nil
}

type callbackReplayRepository struct {
	mu        sync.Mutex
	failFirst bool
	committed bool
	callCount int
	now       time.Time
}

func (r *callbackReplayRepository) ApplyCallbackLocked(_ context.Context, event release.CallbackEvent) (release.PublishJob, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount++
	if r.failFirst && r.callCount == 1 {
		return release.PublishJob{}, false, errors.New("database apply failed")
	}
	duplicate := r.committed
	r.committed = true
	build := event.BuildNumber
	return release.PublishJob{
		ID: event.PublishJobID, ReleaseID: event.ReleaseID, BuilderID: 9,
		BuilderTarget: release.BuilderTargetSnapshot{Name: "Production", BaseURL: "https://jenkins.example.test", Username: "ci", JobName: "blog/deploy"}, Status: event.Status,
		Stage: event.Stage, BuildNumber: &build, CreatedAt: r.now,
	}, duplicate, nil
}

func (r *callbackReplayRepository) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.callCount
}

func (*callbackReplayRepository) CreateLocked(context.Context, release.CreateCommand) (release.Release, release.PublishJob, error) {
	return release.Release{}, release.PublishJob{}, errors.New("not configured")
}
func (*callbackReplayRepository) FindRelease(context.Context, int64) (release.Aggregate, error) {
	return release.Aggregate{}, errors.New("not configured")
}
func (*callbackReplayRepository) ListReleases(context.Context, release.ListQuery) ([]release.Aggregate, error) {
	return nil, errors.New("not configured")
}
func (*callbackReplayRepository) LoadBundleSnapshot(context.Context, int64) (release.Aggregate, release.Bundle, error) {
	return release.Aggregate{}, release.Bundle{}, errors.New("not configured")
}
func (*callbackReplayRepository) CreateRetryLocked(context.Context, int64, int64, release.BuilderTargetSnapshot) (release.Aggregate, release.PublishJob, error) {
	return release.Aggregate{}, release.PublishJob{}, errors.New("not configured")
}
func (*callbackReplayRepository) FailTriggerLocked(context.Context, int64, string, time.Time) (release.PublishJob, bool, error) {
	return release.PublishJob{}, false, errors.New("not configured")
}
func (*callbackReplayRepository) ReconcileLocked(context.Context, release.Artifact) (bool, error) {
	return false, errors.New("not configured")
}

var _ release.Repository = (*callbackReplayRepository)(nil)
var _ release.BuilderTargetProvider = callbackReplayBuilder{}
