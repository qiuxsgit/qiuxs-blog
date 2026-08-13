package tag_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
	"github.com/stretchr/testify/require"
)

func TestServiceCreateNormalizesNameAndRetriesOnlySlugConflict(t *testing.T) {
	keys := newTagKeys(t, bytes.NewReader(append(bytes.Repeat([]byte{0}, 12), bytes.Repeat([]byte{1}, 12)...)))
	now := time.Date(2026, 8, 13, 18, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	var calls []createCall
	repository := &fakeRepository{
		create: func(_ context.Context, name, slug string, at time.Time) (tag.Tag, error) {
			calls = append(calls, createCall{name: name, slug: slug, at: at})
			if len(calls) == 1 {
				return tag.Tag{}, tag.ErrSlugConflict
			}
			return tag.Tag{ID: 41, Name: name, Slug: slug, CreatedAt: at, UpdatedAt: at}, nil
		},
	}
	service, err := tag.NewService(repository, keys, func() time.Time { return now })
	require.NoError(t, err)

	got, err := service.Create(context.Background(), "  Café\t au\n lait  ")

	require.NoError(t, err)
	require.Equal(t, tag.Tag{
		ID:        41,
		Name:      "Café au lait",
		Slug:      "t_bbbbbbbbbbbb",
		CreatedAt: now.UTC(),
		UpdatedAt: now.UTC(),
	}, got)
	require.Equal(t, []createCall{
		{name: "Café au lait", slug: "t_aaaaaaaaaaaa", at: now.UTC()},
		{name: "Café au lait", slug: "t_bbbbbbbbbbbb", at: now.UTC()},
	}, calls)
}

func TestServiceCreateRejectsInvalidNormalizedNameBeforeRandomOrRepositoryUse(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "empty", value: " \t\n "},
		{name: "over 64 Unicode code points", value: strings.Repeat("界", 65)},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{}
			keys := newTagKeys(t, bytes.NewReader(bytes.Repeat([]byte{255}, 1024)))
			service, err := tag.NewService(repository, keys, time.Now)
			require.NoError(t, err)

			got, err := service.Create(context.Background(), test.value)

			require.Equal(t, tag.Tag{}, got)
			require.ErrorIs(t, err, tag.ErrInvalidName)
			require.Zero(t, repository.createCalls)
			require.NotContains(t, err.Error(), test.value)
		})
	}
}

func TestServiceCreateAllowsExactly64UnicodeCodePoints(t *testing.T) {
	name := strings.Repeat("界", 64)
	repository := &fakeRepository{create: func(_ context.Context, gotName, slug string, at time.Time) (tag.Tag, error) {
		return tag.Tag{ID: 1, Name: gotName, Slug: slug, CreatedAt: at, UpdatedAt: at}, nil
	}}
	service, err := tag.NewService(repository, newTagKeys(t, bytes.NewReader(bytes.Repeat([]byte{0}, 12))), time.Now)
	require.NoError(t, err)

	got, err := service.Create(context.Background(), name)

	require.NoError(t, err)
	require.Equal(t, name, got.Name)
}

func TestServiceCreateStopsAfterFiveSlugConflictRetries(t *testing.T) {
	repository := &fakeRepository{create: func(context.Context, string, string, time.Time) (tag.Tag, error) {
		return tag.Tag{}, tag.ErrSlugConflict
	}}
	service, err := tag.NewService(repository, newTagKeys(t, bytes.NewReader(bytes.Repeat([]byte{0}, 72))), time.Now)
	require.NoError(t, err)

	got, err := service.Create(context.Background(), "Go")

	require.Equal(t, tag.Tag{}, got)
	require.ErrorIs(t, err, tag.ErrSlugConflict)
	require.Equal(t, 6, repository.createCalls)
}

func TestServiceCreateDoesNotRetryNameConflictOrDependencyFailure(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure error
		want    error
	}{
		{name: "name conflict", failure: tag.ErrNameConflict, want: tag.ErrNameConflict},
		{name: "dependency", failure: errors.New("repository-name-slug-secret"), want: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{create: func(context.Context, string, string, time.Time) (tag.Tag, error) {
				return tag.Tag{}, test.failure
			}}
			service, err := tag.NewService(repository, newTagKeys(t, bytes.NewReader(bytes.Repeat([]byte{0}, 72))), time.Now)
			require.NoError(t, err)

			_, err = service.Create(context.Background(), "Go")

			require.Error(t, err)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
			}
			require.NotContains(t, err.Error(), "repository-name-slug-secret")
			require.Equal(t, 1, repository.createCalls)
		})
	}
}

func TestServiceCreateSanitizesRandomSourceFailure(t *testing.T) {
	repository := &fakeRepository{}
	keys := newTagKeys(t, errorReader{})
	service, err := tag.NewService(repository, keys, time.Now)
	require.NoError(t, err)

	_, err = service.Create(context.Background(), "Go")

	require.Error(t, err)
	require.NotContains(t, err.Error(), "random-key-secret")
	require.Zero(t, repository.createCalls)
}

func TestServiceListReturnsRepositoryOrderAndSanitizesFailure(t *testing.T) {
	want := []tag.Tag{{ID: 2, Name: "Go", Slug: "t_go"}, {ID: 1, Name: "Rust", Slug: "t_rust"}}
	repository := &fakeRepository{list: func(context.Context) ([]tag.Tag, error) { return want, nil }}
	service, err := tag.NewService(repository, newTagKeys(t, bytes.NewReader(nil)), time.Now)
	require.NoError(t, err)

	got, err := service.List(context.Background())

	require.NoError(t, err)
	require.Equal(t, want, got)

	repository.list = func(context.Context) ([]tag.Tag, error) { return nil, errors.New("list-secret") }
	_, err = service.List(context.Background())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "list-secret")
}

func TestServiceRenameNormalizesNameAndPreservesStoredSlug(t *testing.T) {
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	repository := &fakeRepository{rename: func(_ context.Context, id int64, name string, at time.Time) (tag.Tag, error) {
		require.Equal(t, int64(41), id)
		require.Equal(t, "Modern Go", name)
		require.Equal(t, now.UTC(), at)
		return tag.Tag{ID: id, Name: name, Slug: "t_stable_slug", UpdatedAt: at}, nil
	}}
	service, err := tag.NewService(repository, newTagKeys(t, bytes.NewReader(nil)), func() time.Time { return now })
	require.NoError(t, err)

	got, err := service.Rename(context.Background(), 41, "  Modern\tGo ")

	require.NoError(t, err)
	require.Equal(t, "Modern Go", got.Name)
	require.Equal(t, "t_stable_slug", got.Slug)
	require.Equal(t, 1, repository.renameCalls)
}

func TestServiceRenameRejectsInvalidInputWithoutRepositoryUse(t *testing.T) {
	repository := &fakeRepository{}
	service, err := tag.NewService(repository, newTagKeys(t, bytes.NewReader(nil)), time.Now)
	require.NoError(t, err)

	for _, call := range []struct {
		name  string
		id    int64
		value string
		want  error
	}{
		{name: "nonpositive ID", id: 0, value: "Go", want: tag.ErrInvalidSelection},
		{name: "invalid name", id: 1, value: " ", want: tag.ErrInvalidName},
	} {
		t.Run(call.name, func(t *testing.T) {
			_, callErr := service.Rename(context.Background(), call.id, call.value)
			require.ErrorIs(t, callErr, call.want)
		})
	}
	require.Zero(t, repository.renameCalls)
}

func TestServiceSnapshotsReordersRepositoryResultsToSubmittedIDs(t *testing.T) {
	repository := &fakeRepository{findByIDs: func(_ context.Context, ids []int64) ([]tag.Tag, error) {
		require.Equal(t, []int64{9, 2}, ids)
		return []tag.Tag{
			{ID: 2, Name: "Rust", Slug: "t_rust"},
			{ID: 9, Name: "Go", Slug: "t_go"},
		}, nil
	}}
	service, err := tag.NewService(repository, newTagKeys(t, bytes.NewReader(nil)), time.Now)
	require.NoError(t, err)

	got, err := service.Snapshots(context.Background(), []int64{9, 2})

	require.NoError(t, err)
	require.Equal(t, []tag.Snapshot{
		{TagID: 9, Name: "Go", Slug: "t_go", Position: 0},
		{TagID: 2, Name: "Rust", Slug: "t_rust", Position: 1},
	}, got)
}

func TestServiceSnapshotsRejectsDuplicatesMissingAndInvalidIDs(t *testing.T) {
	tests := []struct {
		name      string
		ids       []int64
		results   []tag.Tag
		want      error
		wantCalls int
	}{
		{name: "duplicate", ids: []int64{2, 2}, want: tag.ErrInvalidSelection},
		{name: "nonpositive", ids: []int64{0}, want: tag.ErrInvalidSelection},
		{name: "missing", ids: []int64{9, 2}, results: []tag.Tag{{ID: 9, Name: "Go", Slug: "t_go"}}, want: tag.ErrNotFound, wantCalls: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{findByIDs: func(context.Context, []int64) ([]tag.Tag, error) { return test.results, nil }}
			service, err := tag.NewService(repository, newTagKeys(t, bytes.NewReader(nil)), time.Now)
			require.NoError(t, err)

			got, callErr := service.Snapshots(context.Background(), test.ids)

			require.Nil(t, got)
			require.ErrorIs(t, callErr, test.want)
			require.Equal(t, test.wantCalls, repository.findCalls)
		})
	}
}

func TestServiceSnapshotsReturnsEmptyOrderedValueWithoutRepositoryCall(t *testing.T) {
	repository := &fakeRepository{}
	service, err := tag.NewService(repository, newTagKeys(t, bytes.NewReader(nil)), time.Now)
	require.NoError(t, err)

	got, err := service.Snapshots(context.Background(), nil)

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)
	require.Zero(t, repository.findCalls)
}

func TestNewServiceRejectsNilDependenciesAndMethodsRejectNilContext(t *testing.T) {
	keys := newTagKeys(t, bytes.NewReader(nil))
	var typedNilRepository *fakeRepository
	for _, test := range []struct {
		name       string
		repository tag.Repository
		keys       *randomkey.Generator
		now        func() time.Time
	}{
		{name: "nil repository", keys: keys, now: time.Now},
		{name: "typed nil repository", repository: typedNilRepository, keys: keys, now: time.Now},
		{name: "nil keys", repository: &fakeRepository{}, now: time.Now},
		{name: "nil clock", repository: &fakeRepository{}, keys: keys},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				service, err := tag.NewService(test.repository, test.keys, test.now)
				require.Nil(t, service)
				require.Error(t, err)
			})
		})
	}

	repository := &fakeRepository{}
	service, err := tag.NewService(repository, keys, time.Now)
	require.NoError(t, err)
	_, err = service.Create(nil, "Go")
	require.ErrorIs(t, err, tag.ErrInvalidSelection)
	_, err = service.List(nil)
	require.ErrorIs(t, err, tag.ErrInvalidSelection)
	_, err = service.Rename(nil, 1, "Go")
	require.ErrorIs(t, err, tag.ErrInvalidSelection)
	_, err = service.Snapshots(nil, []int64{1})
	require.ErrorIs(t, err, tag.ErrInvalidSelection)
	require.Zero(t, repository.createCalls)
	require.Zero(t, repository.listCalls)
	require.Zero(t, repository.renameCalls)
	require.Zero(t, repository.findCalls)
}

type createCall struct {
	name string
	slug string
	at   time.Time
}

type fakeRepository struct {
	create      func(context.Context, string, string, time.Time) (tag.Tag, error)
	list        func(context.Context) ([]tag.Tag, error)
	rename      func(context.Context, int64, string, time.Time) (tag.Tag, error)
	findByIDs   func(context.Context, []int64) ([]tag.Tag, error)
	createCalls int
	listCalls   int
	renameCalls int
	findCalls   int
}

func (r *fakeRepository) Create(ctx context.Context, name, slug string, at time.Time) (tag.Tag, error) {
	r.createCalls++
	if r.create == nil {
		return tag.Tag{}, errors.New("unexpected create")
	}
	return r.create(ctx, name, slug, at)
}

func (r *fakeRepository) List(ctx context.Context) ([]tag.Tag, error) {
	r.listCalls++
	if r.list == nil {
		return nil, errors.New("unexpected list")
	}
	return r.list(ctx)
}

func (r *fakeRepository) Rename(ctx context.Context, id int64, name string, at time.Time) (tag.Tag, error) {
	r.renameCalls++
	if r.rename == nil {
		return tag.Tag{}, errors.New("unexpected rename")
	}
	return r.rename(ctx, id, name, at)
}

func (r *fakeRepository) FindByIDs(ctx context.Context, ids []int64) ([]tag.Tag, error) {
	r.findCalls++
	if r.findByIDs == nil {
		return nil, errors.New("unexpected find")
	}
	return r.findByIDs(ctx, ids)
}

func newTagKeys(t *testing.T, reader interface{ Read([]byte) (int, error) }) *randomkey.Generator {
	t.Helper()
	keys, err := randomkey.New(reader)
	require.NoError(t, err)
	return keys
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("random-key-secret") }
