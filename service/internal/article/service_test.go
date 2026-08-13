package article

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
	"github.com/stretchr/testify/require"
)

func TestServiceCreateGeneratesStableSlugAndReturnsInitialDraft(t *testing.T) {
	at := time.Date(2026, 8, 14, 9, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	repository := newArticleRepositoryFake()
	repository.createResults = []articleResult{{article: Article{
		ID: 11, Slug: "aaaaaaaaaaaa", DraftRevisionID: 21, State: StateActive,
		CreatedAt: at.UTC(), UpdatedAt: at.UTC(),
	}}}
	draft := revision.Draft{
		ID: 21, ArticleID: 11, RevisionNo: 1, LockVersion: 1,
		Status: revision.StatusEditing, Reason: revision.ReasonDraft,
		ContentHash: "9ebca1a33e28c44890c99e46d508488363522c83f17a31056641ad11b11a153f",
		CreatedAt:   at.UTC(), UpdatedAt: at.UTC(),
	}
	drafts := &draftReaderFake{byArticleID: map[int64]revision.Draft{11: draft}}
	service := newArticleService(t, repository, drafts, bytes.Repeat([]byte{0}, 12), func() time.Time { return at })

	got, err := service.Create(context.Background())

	require.NoError(t, err)
	require.Equal(t, Detail{Article: repository.createResults[0].article, Draft: draft}, got)
	require.Equal(t, []string{"aaaaaaaaaaaa"}, repository.createSlugs)
	require.Equal(t, []time.Time{at.UTC()}, repository.createTimes)
	require.Equal(t, []int64{11}, drafts.calls)
}

func TestServiceCreateRetriesOnlySlugConflictsAtMostFiveTimes(t *testing.T) {
	t.Run("success after conflict", func(t *testing.T) {
		repository := newArticleRepositoryFake()
		repository.createResults = []articleResult{{err: ErrSlugConflict}, {article: Article{ID: 11, Slug: "bbbbbbbbbbbb", DraftRevisionID: 21, State: StateActive}}}
		drafts := &draftReaderFake{byArticleID: map[int64]revision.Draft{11: {ID: 21, ArticleID: 11}}}
		service := newArticleService(t, repository, drafts, append(bytes.Repeat([]byte{0}, 12), bytes.Repeat([]byte{1}, 12)...), time.Now)

		got, err := service.Create(context.Background())

		require.NoError(t, err)
		require.Equal(t, "bbbbbbbbbbbb", got.Article.Slug)
		require.Equal(t, []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"}, repository.createSlugs)
	})

	t.Run("stops after five retries", func(t *testing.T) {
		repository := newArticleRepositoryFake()
		for range 6 {
			repository.createResults = append(repository.createResults, articleResult{err: ErrSlugConflict})
		}
		service := newArticleService(t, repository, &draftReaderFake{}, bytes.Repeat([]byte{0}, 12*6), time.Now)

		_, err := service.Create(context.Background())

		require.ErrorIs(t, err, ErrSlugConflict)
		require.Len(t, repository.createSlugs, 6)
	})

	t.Run("dependency error does not retry", func(t *testing.T) {
		repository := newArticleRepositoryFake()
		repository.createResults = []articleResult{{err: errors.New("mysql-slug-secret")}}
		service := newArticleService(t, repository, &draftReaderFake{}, bytes.Repeat([]byte{0}, 24), time.Now)

		_, err := service.Create(context.Background())

		require.Error(t, err)
		require.NotContains(t, err.Error(), "mysql-slug-secret")
		require.Len(t, repository.createSlugs, 1)
	})
}

func TestServiceCreateSanitizesRandomAndDraftReadFailures(t *testing.T) {
	t.Run("random", func(t *testing.T) {
		service := newArticleService(t, newArticleRepositoryFake(), &draftReaderFake{}, nil, time.Now)

		_, err := service.Create(context.Background())

		require.Error(t, err)
		require.NotContains(t, err.Error(), "EOF")
	})

	t.Run("draft reader", func(t *testing.T) {
		repository := newArticleRepositoryFake()
		repository.createResults = []articleResult{{article: Article{ID: 11, Slug: "aaaaaaaaaaaa", DraftRevisionID: 21, State: StateActive}}}
		drafts := &draftReaderFake{err: errors.New("draft-content-secret")}
		service := newArticleService(t, repository, drafts, bytes.Repeat([]byte{0}, 12), time.Now)

		_, err := service.Create(context.Background())

		require.Error(t, err)
		require.NotContains(t, err.Error(), "draft-content-secret")
	})
}

func TestServiceGetCombinesArticleAndCurrentDraft(t *testing.T) {
	article := Article{ID: 11, Slug: "aaaaaaaaaaaa", DraftRevisionID: 21, State: StateActive}
	draft := revision.Draft{ID: 21, ArticleID: 11, Title: "Draft"}
	repository := newArticleRepositoryFake()
	repository.byID[11] = article
	drafts := &draftReaderFake{byArticleID: map[int64]revision.Draft{11: draft}}
	service := newArticleService(t, repository, drafts, bytes.Repeat([]byte{0}, 12), time.Now)

	got, err := service.Get(context.Background(), 11)

	require.NoError(t, err)
	require.Equal(t, Detail{Article: article, Draft: draft}, got)
	require.Equal(t, []int64{11}, repository.findCalls)
	require.Equal(t, []int64{11}, drafts.calls)
}

func TestServiceListAcceptsOnlyExplicitStatesAndPreservesRepositoryOrder(t *testing.T) {
	repository := newArticleRepositoryFake()
	repository.listResults[StateActive] = []Summary{
		{Article: Article{ID: 9, State: StateActive}, DraftTitle: "Newest", DraftUpdatedAt: time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)},
		{Article: Article{ID: 2, State: StateActive}, DraftTitle: "Older", DraftUpdatedAt: time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC)},
	}
	service := newArticleService(t, repository, &draftReaderFake{}, bytes.Repeat([]byte{0}, 12), time.Now)

	got, err := service.List(context.Background(), StateActive)

	require.NoError(t, err)
	require.Equal(t, repository.listResults[StateActive], got)
	require.Equal(t, []State{StateActive}, repository.listCalls)

	for _, state := range []State{"", "deleted", "ACTIVE"} {
		_, err = service.List(context.Background(), state)
		require.ErrorIs(t, err, ErrStateConflict)
	}
	require.Equal(t, []State{StateActive}, repository.listCalls)
}

func TestServiceTrashRejectsPublishedOrNonActiveArticleBeforeUpdate(t *testing.T) {
	publishedID := int64(31)
	for _, test := range []struct {
		name    string
		article Article
		want    error
	}{
		{name: "published", article: Article{ID: 11, State: StateActive, PublishedRevisionID: &publishedID}, want: ErrMustBeUnpublished},
		{name: "already trashed", article: Article{ID: 11, State: StateTrashed}, want: ErrStateConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := newArticleRepositoryFake()
			repository.byID[11] = test.article
			service := newArticleService(t, repository, &draftReaderFake{}, bytes.Repeat([]byte{0}, 12), time.Now)

			err := service.Trash(context.Background(), 11)

			require.ErrorIs(t, err, test.want)
			require.Empty(t, repository.setStateCalls)
		})
	}
}

func TestServiceTrashTransitionsUnpublishedActiveArticleAtInjectedUTC(t *testing.T) {
	at := time.Date(2026, 8, 14, 9, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	repository := newArticleRepositoryFake()
	repository.byID[11] = Article{ID: 11, State: StateActive}
	service := newArticleService(t, repository, &draftReaderFake{}, bytes.Repeat([]byte{0}, 12), func() time.Time { return at })

	err := service.Trash(context.Background(), 11)

	require.NoError(t, err)
	require.Equal(t, []stateCall{{id: 11, from: StateActive, to: StateTrashed, at: at.UTC()}}, repository.setStateCalls)
}

func TestServiceUntrashTransitionsOnlyTrashedArticle(t *testing.T) {
	at := time.Date(2026, 8, 14, 9, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	t.Run("trashed", func(t *testing.T) {
		repository := newArticleRepositoryFake()
		repository.byID[11] = Article{ID: 11, State: StateTrashed}
		service := newArticleService(t, repository, &draftReaderFake{}, bytes.Repeat([]byte{0}, 12), func() time.Time { return at })

		err := service.Untrash(context.Background(), 11)

		require.NoError(t, err)
		require.Equal(t, []stateCall{{id: 11, from: StateTrashed, to: StateActive, at: at.UTC()}}, repository.setStateCalls)
	})

	t.Run("already active", func(t *testing.T) {
		repository := newArticleRepositoryFake()
		repository.byID[11] = Article{ID: 11, State: StateActive}
		service := newArticleService(t, repository, &draftReaderFake{}, bytes.Repeat([]byte{0}, 12), time.Now)

		err := service.Untrash(context.Background(), 11)

		require.ErrorIs(t, err, ErrStateConflict)
		require.Empty(t, repository.setStateCalls)
	})
}

func TestServicePreservesTransitionRaceErrorsAndSanitizesDependencies(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "state race", err: ErrStateConflict, want: ErrStateConflict},
		{name: "published race", err: ErrMustBeUnpublished, want: ErrMustBeUnpublished},
		{name: "missing race", err: ErrNotFound, want: ErrNotFound},
		{name: "dependency", err: errors.New("transition-secret")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := newArticleRepositoryFake()
			repository.byID[11] = Article{ID: 11, State: StateActive}
			repository.setStateErr = test.err
			service := newArticleService(t, repository, &draftReaderFake{}, bytes.Repeat([]byte{0}, 12), time.Now)

			err := service.Trash(context.Background(), 11)

			require.Error(t, err)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
			}
			require.NotContains(t, err.Error(), "transition-secret")
		})
	}
}

func TestNewServiceRejectsNilDependenciesAndMethodsAreNilSafe(t *testing.T) {
	repository := newArticleRepositoryFake()
	drafts := &draftReaderFake{}
	keys := newArticleKeys(t, bytes.Repeat([]byte{0}, 12))
	var typedNilRepository *articleRepositoryFake
	var typedNilDrafts *draftReaderFake

	for _, test := range []struct {
		name       string
		repository Repository
		drafts     DraftReader
		keys       *randomkey.Generator
		now        func() time.Time
	}{
		{name: "nil repository", drafts: drafts, keys: keys, now: time.Now},
		{name: "typed nil repository", repository: typedNilRepository, drafts: drafts, keys: keys, now: time.Now},
		{name: "nil drafts", repository: repository, keys: keys, now: time.Now},
		{name: "typed nil drafts", repository: repository, drafts: typedNilDrafts, keys: keys, now: time.Now},
		{name: "nil keys", repository: repository, drafts: drafts, now: time.Now},
		{name: "nil clock", repository: repository, drafts: drafts, keys: keys},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewService(test.repository, test.drafts, test.keys, test.now)
			require.Nil(t, service)
			require.Error(t, err)
		})
	}

	var nilService *service
	require.NotPanics(t, func() {
		_, err := nilService.Get(context.Background(), 11)
		require.Error(t, err)
	})
	valid := newArticleService(t, repository, drafts, bytes.Repeat([]byte{0}, 12), time.Now)
	require.NotPanics(t, func() {
		_, err := valid.Get(nil, 11)
		require.Error(t, err)
	})
}

type articleResult struct {
	article Article
	err     error
}

type stateCall struct {
	id       int64
	from, to State
	at       time.Time
}

type articleRepositoryFake struct {
	createResults []articleResult
	createSlugs   []string
	createTimes   []time.Time
	byID          map[int64]Article
	findErr       error
	findCalls     []int64
	listResults   map[State][]Summary
	listErr       error
	listCalls     []State
	setStateCalls []stateCall
	setStateErr   error
}

func newArticleRepositoryFake() *articleRepositoryFake {
	return &articleRepositoryFake{byID: make(map[int64]Article), listResults: make(map[State][]Summary)}
}

func (r *articleRepositoryFake) Create(_ context.Context, slug string, at time.Time) (Article, error) {
	r.createSlugs = append(r.createSlugs, slug)
	r.createTimes = append(r.createTimes, at)
	index := len(r.createSlugs) - 1
	if index < len(r.createResults) {
		return r.createResults[index].article, r.createResults[index].err
	}
	return Article{}, errors.New("unexpected create")
}

func (r *articleRepositoryFake) FindByID(_ context.Context, id int64) (Article, error) {
	r.findCalls = append(r.findCalls, id)
	if r.findErr != nil {
		return Article{}, r.findErr
	}
	item, exists := r.byID[id]
	if !exists {
		return Article{}, ErrNotFound
	}
	return item, nil
}

func (r *articleRepositoryFake) List(_ context.Context, state State) ([]Summary, error) {
	r.listCalls = append(r.listCalls, state)
	return append([]Summary(nil), r.listResults[state]...), r.listErr
}

func (r *articleRepositoryFake) SetState(_ context.Context, id int64, from, to State, at time.Time) error {
	r.setStateCalls = append(r.setStateCalls, stateCall{id: id, from: from, to: to, at: at})
	return r.setStateErr
}

type draftReaderFake struct {
	byArticleID map[int64]revision.Draft
	err         error
	calls       []int64
}

func (r *draftReaderFake) GetDraft(_ context.Context, articleID int64) (revision.Draft, error) {
	r.calls = append(r.calls, articleID)
	if r.err != nil {
		return revision.Draft{}, r.err
	}
	draft, exists := r.byArticleID[articleID]
	if !exists {
		return revision.Draft{}, revision.ErrNotFound
	}
	return draft, nil
}

func newArticleService(t *testing.T, repository Repository, drafts DraftReader, random []byte, now func() time.Time) Service {
	t.Helper()
	keys := newArticleKeys(t, random)
	service, err := NewService(repository, drafts, keys, now)
	require.NoError(t, err)
	return service
}

func newArticleKeys(t *testing.T, random []byte) *randomkey.Generator {
	t.Helper()
	keys, err := randomkey.New(bytes.NewReader(random))
	require.NoError(t, err)
	return keys
}
