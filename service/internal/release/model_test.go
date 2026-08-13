package release

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
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
		Tags: []BundleTag{{ID: 1, Name: "Go", Slug: "go"}},
		Articles: []BundleArticle{{
			ArticleID: 2, RevisionID: 3, Slug: "example", Title: "Title", Summary: "Summary",
			ContentMarkdown: "Body", ContentHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			PublishedAt: generatedAt, Tags: []string{"go"},
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
	event := CallbackEvent{ReleaseID: 7, BuildNumber: 12, Stage: "deploy", Status: JobDeploying, Timestamp: time.Now(), Nonce: "abcdefghijklmnop"}
	require.Equal(t, int64(7), event.ReleaseID)
	artifact := Artifact{ReleaseID: 7, BuildNumber: 12, DeployedAt: time.Now()}
	require.Equal(t, int64(12), artifact.BuildNumber)
	require.ErrorIs(t, ErrBusy, ErrBusy)
	require.ErrorIs(t, ErrNotFound, ErrNotFound)
	require.ErrorIs(t, ErrConflict, ErrConflict)
	require.ErrorIs(t, ErrReconciliationRequired, ErrReconciliationRequired)
}

func mapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	return keys
}

type repositoryContractFake struct{}

func (*repositoryContractFake) CreateLocked(context.Context, CreateCommand) (Release, PublishJob, error) {
	return Release{}, PublishJob{}, errors.New("not implemented")
}

func (*repositoryContractFake) FindRelease(context.Context, int64) (Release, error) {
	return Release{}, errors.New("not implemented")
}

func (*repositoryContractFake) ListReleases(context.Context) ([]Release, error) {
	return nil, errors.New("not implemented")
}

func (*repositoryContractFake) LoadBundle(context.Context, int64) (Bundle, error) {
	return Bundle{}, errors.New("not implemented")
}

func (*repositoryContractFake) CreateRetryLocked(context.Context, int64) (PublishJob, error) {
	return PublishJob{}, errors.New("not implemented")
}

func (*repositoryContractFake) ApplyCallbackLocked(context.Context, CallbackEvent) (PublishJob, bool, error) {
	return PublishJob{}, false, errors.New("not implemented")
}

func (*repositoryContractFake) ReconcileLocked(context.Context, Artifact) (bool, error) {
	return false, errors.New("not implemented")
}
