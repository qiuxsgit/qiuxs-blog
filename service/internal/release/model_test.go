package release

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReleaseAndJobStatusesAreExactCaseSensitiveMachineValues(t *testing.T) {
	require.Equal(t, []ReleaseStatus{ReleaseQueued, ReleaseSuccess, ReleaseFailed}, []ReleaseStatus{"queued", "success", "failed"})
	require.Equal(t, []JobStatus{JobPending, JobQueued, JobBuilding, JobDeploying, JobSuccess, JobFailed}, []JobStatus{"pending", "queued", "building", "deploying", "success", "failed"})
	require.Equal(t, []PublishMode{PublishArticle, UnpublishArticle, PublishSettings}, []PublishMode{"publish_article", "unpublish_article", "publish_settings"})
}

func TestReleaseBundleMarshalsOnlyVersionedPublicSnapshotFieldsWithSignedIDs(t *testing.T) {
	generatedAt := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	bundle := Bundle{
		SchemaVersion: 1,
		ReleaseID:     7,
		GeneratedAt:   generatedAt,
		Site: BundleSite{
			Name: "Blog", AuthorBio: "Bio", AboutMarkdown: "About",
			FilingName: "长安休息室", FilingNumber: "浙ICP备17057726号-1",
			SocialLinks: []SocialLink{{Label: "GitHub", URL: "https://github.com/qiuxsgit"}},
		},
		Tags: []BundleTag{{ID: 1, Name: "Go", Slug: "t_abcdefghijkl"}},
		Articles: []BundleArticle{{
			ArticleID: 2, RevisionID: 3, Slug: "example", Title: "Title", Summary: "Summary",
			ContentMarkdown: "Body", ContentHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			PublishedAt: generatedAt, Tags: []string{"t_abcdefghijkl"},
		}},
		Checksum: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}

	raw, err := json.Marshal(bundle)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.ElementsMatch(t, []string{"schemaVersion", "releaseId", "generatedAt", "site", "tags", "articles", "checksum"}, mapKeys(decoded))
	require.Equal(t, float64(7), decoded["releaseId"])
	require.NotContains(t, string(raw), "token")
	require.NotContains(t, string(raw), "builder")

	require.Equal(t, reflect.TypeOf(int64(0)), reflect.TypeOf(bundle.ReleaseID))
	require.Equal(t, reflect.TypeOf(int64(0)), reflect.TypeOf(bundle.Tags[0].ID))
	require.Equal(t, reflect.TypeOf(int64(0)), reflect.TypeOf(bundle.Articles[0].ArticleID))
	require.Equal(t, reflect.TypeOf(int64(0)), reflect.TypeOf(bundle.Articles[0].RevisionID))
}

func TestReleaseRepositoryContractUsesImmutableCommandsAndSnapshots(t *testing.T) {
	var _ Repository = (*repositoryContractFake)(nil)
	command := CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9, RequestedBy: 1}
	require.Equal(t, int64(41), command.ArticleID)
	event := CallbackEvent{ReleaseID: 7, PublishJobID: 11, BuildNumber: 12, Stage: "deploy", Status: JobDeploying, Timestamp: time.Now(), Nonce: "abcdefghijklmnop"}
	require.Equal(t, int64(7), event.ReleaseID)
	require.Equal(t, int64(11), event.PublishJobID)
	artifact := Artifact{ReleaseID: 7, BuildNumber: 12, DeployedAt: time.Now()}
	require.Equal(t, int64(12), artifact.BuildNumber)
	require.ErrorIs(t, ErrBusy, ErrBusy)
	require.ErrorIs(t, ErrNotFound, ErrNotFound)
	require.ErrorIs(t, ErrConflict, ErrConflict)
	require.ErrorIs(t, ErrReconciliationRequired, ErrReconciliationRequired)
}

func TestSnapshotSourceContractCarriesExplicitModeTargetAndImmutableBase(t *testing.T) {
	var _ SnapshotSource = (*snapshotSourceContractFake)(nil)
	request := SnapshotRequest{
		Mode:             PublishArticle,
		ArticleID:        41,
		CurrentReleaseID: 7,
		Base: PreparedSnapshot{
			Site:     SiteSnapshot{Name: "current"},
			Articles: []ArticleSnapshot{{ArticleID: 41, RevisionID: 71}},
			Checksum: "sha256:" + strings.Repeat("a", 64),
		},
	}
	require.Equal(t, PublishArticle, request.Mode)
	require.Equal(t, int64(41), request.ArticleID)
	require.Equal(t, int64(7), request.CurrentReleaseID)
}

func TestReleaseAggregateExposesOrderedJobHistoryWithoutAliasing(t *testing.T) {
	createdAt := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	buildNumber := int64(41)
	finishedAt := createdAt.Add(time.Minute)
	aggregate := Aggregate{
		Release: Release{ID: 7},
		Jobs: []PublishJob{
			{ID: 12, ReleaseID: 7, Status: JobBuilding, BuildNumber: &buildNumber, CreatedAt: createdAt, FinishedAt: &finishedAt},
			{ID: 11, ReleaseID: 7, Status: JobFailed, CreatedAt: createdAt.Add(-time.Minute)},
		},
	}

	require.NoError(t, aggregate.Validate())
	latest, err := aggregate.LatestJob()
	require.NoError(t, err)
	require.Equal(t, int64(12), latest.ID)
	latest.Status = JobSuccess
	*latest.BuildNumber = 42
	latestFinishedAt := finishedAt.Add(time.Minute)
	originalFinishedAt := finishedAt.Add(2 * time.Minute)
	*latest.FinishedAt = latestFinishedAt
	require.Equal(t, JobBuilding, aggregate.Jobs[0].Status)
	require.Equal(t, int64(41), *aggregate.Jobs[0].BuildNumber)
	require.Equal(t, finishedAt, *aggregate.Jobs[0].FinishedAt)
	*aggregate.Jobs[0].BuildNumber = 43
	*aggregate.Jobs[0].FinishedAt = originalFinishedAt
	require.Equal(t, int64(42), *latest.BuildNumber)
	require.Equal(t, latestFinishedAt, *latest.FinishedAt)
	require.Equal(t, []int64{12, 11}, []int64{aggregate.Jobs[0].ID, aggregate.Jobs[1].ID})

	empty := Aggregate{Release: Release{ID: 8}, Jobs: []PublishJob{}}
	require.NotNil(t, empty.Jobs)
	_, err = empty.LatestJob()
	require.ErrorIs(t, err, ErrInvalidAggregate)
}

func TestReleaseAggregatePinsRetryIdentityAndDeterministicHistoryOrder(t *testing.T) {
	createdAt := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	newJob := PublishJob{ID: 13, ReleaseID: 7, Status: JobPending, CreatedAt: createdAt}
	aggregate := Aggregate{
		Release: Release{ID: 7},
		Jobs: []PublishJob{
			newJob,
			{ID: 12, ReleaseID: 7, Status: JobFailed, CreatedAt: createdAt},
			{ID: 11, ReleaseID: 7, Status: JobFailed, CreatedAt: createdAt.Add(-time.Minute)},
		},
	}
	require.NoError(t, aggregate.ValidateRetry(newJob))

	mismatched := newJob
	mismatched.ID = 14
	require.ErrorIs(t, aggregate.ValidateRetry(mismatched), ErrInvalidAggregate)

	for name, jobs := range map[string][]PublishJob{
		"newer timestamp after older": {aggregate.Jobs[1], aggregate.Jobs[0]},
		"higher id after lower tie":   {aggregate.Jobs[1], aggregate.Jobs[0], aggregate.Jobs[2]},
		"foreign release job":         {{ID: 13, ReleaseID: 8, CreatedAt: createdAt}},
	} {
		t.Run(name, func(t *testing.T) {
			invalid := Aggregate{Release: Release{ID: 7}, Jobs: jobs}
			require.ErrorIs(t, invalid.Validate(), ErrInvalidAggregate)
		})
	}
}

func mapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	return keys
}

type repositoryContractFake struct{}

type snapshotSourceContractFake struct{}

func (*snapshotSourceContractFake) PrepareSnapshot(context.Context, SnapshotExecutor, SnapshotRequest) (PreparedSnapshot, error) {
	return PreparedSnapshot{}, errors.New("not implemented")
}

func (*repositoryContractFake) CreateLocked(context.Context, CreateCommand) (Release, PublishJob, error) {
	return Release{}, PublishJob{}, errors.New("not implemented")
}

func (*repositoryContractFake) FindRelease(context.Context, int64) (Aggregate, error) {
	return Aggregate{}, errors.New("not implemented")
}

func (*repositoryContractFake) ListReleases(context.Context, ListQuery) ([]Aggregate, error) {
	return nil, errors.New("not implemented")
}

func (*repositoryContractFake) LoadBundleSnapshot(context.Context, int64) (Aggregate, Bundle, error) {
	return Aggregate{}, Bundle{}, errors.New("not implemented")
}

func (*repositoryContractFake) CreateRetryLocked(context.Context, int64, int64, BuilderTargetSnapshot) (Aggregate, PublishJob, error) {
	return Aggregate{}, PublishJob{}, errors.New("not implemented")
}

func (*repositoryContractFake) ApplyCallbackLocked(context.Context, CallbackEvent) (PublishJob, bool, error) {
	return PublishJob{}, false, errors.New("not implemented")
}

func (*repositoryContractFake) FailTriggerLocked(context.Context, int64, string, time.Time) (PublishJob, bool, error) {
	return PublishJob{}, false, errors.New("not implemented")
}

func (*repositoryContractFake) ReconcileLocked(context.Context, Artifact) (bool, error) {
	return false, errors.New("not implemented")
}
