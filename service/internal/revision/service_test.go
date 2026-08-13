package revision

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
	"github.com/stretchr/testify/require"
)

func TestServiceSaveDraftPreparesResolvedContentBeforeRepositoryMutation(t *testing.T) {
	at := time.Date(2026, 8, 14, 9, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	coverID := int64(91)
	contentMD := "![first](/img/proxy/" + firstMediaKey + ")\n![second](/img/proxy/" + secondMediaKey + ")"
	content := Content{
		Title: "  Title  ", Summary: "\nSummary ", CoverMediaID: &coverID,
		ContentMD: contentMD, TagIDs: []int64{7, 3},
	}
	snapshots := []tag.Snapshot{
		{TagID: 7, Name: "Go", Slug: "t_go", Position: 0},
		{TagID: 3, Name: "Web", Slug: "t_web", Position: 1},
	}
	cover := media.Media{ID: coverID, PublicKey: firstMediaKey, State: "active"}
	references := []media.Reference{
		{MediaID: coverID, PublicKey: firstMediaKey, Purpose: "cover", Position: 0},
		{MediaID: 92, PublicKey: firstMediaKey, Purpose: "content", Position: 1},
		{MediaID: 93, PublicKey: secondMediaKey, Purpose: "content", Position: 2},
	}
	want := Draft{
		ID: 21, ArticleID: 11, RevisionNo: 2, LockVersion: 4,
		Status: StatusEditing, Reason: ReasonDraft,
		Title: "Title", Summary: "Summary", CoverMediaID: &coverID, ContentMD: contentMD,
		ContentHash: "5b732fcfb7289a73704164ad25aaae5be4b188172d1a47932428a8d1cdc7d2dc",
		Tags:        snapshots, Media: references, CreatedAt: at.UTC().Add(-time.Hour), UpdatedAt: at.UTC(),
	}
	order := make([]string, 0)
	repository := &revisionRepositoryFake{saveResult: want, order: &order}
	tags := &tagResolverFake{snapshots: snapshots, order: &order}
	mediaResolver := &mediaResolverFake{cover: cover, references: references, order: &order}
	service := newRevisionService(t, repository, tags, mediaResolver, func() time.Time { return at })

	got, err := service.SaveDraft(context.Background(), 11, 3, content)

	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, []string{"tags", "media", "repository"}, order)
	require.Equal(t, [][]int64{{7, 3}}, tags.calls)
	require.Equal(t, []mediaResolveCall{{coverID: &coverID, publicKeys: []string{firstMediaKey, secondMediaKey}}}, mediaResolver.calls)
	require.Equal(t, []revisionSaveCall{{
		articleID: 11, lockVersion: 3, at: at.UTC(),
		content: PreparedContent{
			Title: "Title", Summary: "Summary", Cover: &cover, ContentMD: contentMD,
			Tags: snapshots, Media: references,
			ContentHash: "5b732fcfb7289a73704164ad25aaae5be4b188172d1a47932428a8d1cdc7d2dc",
		},
	}}, repository.saveCalls)
	require.Equal(t, "  Title  ", content.Title)
}

func TestServiceSaveDraftRejectsInvalidInputBeforeResolution(t *testing.T) {
	for _, test := range []struct {
		name      string
		articleID int64
		lock      int64
		content   Content
	}{
		{name: "article ID", lock: 1, content: Content{Title: "Draft"}},
		{name: "lock zero", articleID: 11, content: Content{Title: "Draft"}},
		{name: "lock negative", articleID: 11, lock: -1, content: Content{Title: "Draft"}},
		{name: "raw HTML", articleID: 11, lock: 1, content: Content{Title: "Draft", ContentMD: "<script>x</script>"}},
		{name: "title too long", articleID: 11, lock: 1, content: Content{Title: string(make([]rune, 201))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &revisionRepositoryFake{}
			tags := &tagResolverFake{}
			mediaResolver := &mediaResolverFake{}
			service := newRevisionService(t, repository, tags, mediaResolver, time.Now)

			_, err := service.SaveDraft(context.Background(), test.articleID, test.lock, test.content)

			require.ErrorIs(t, err, ErrInvalidContent)
			require.Empty(t, tags.calls)
			require.Empty(t, mediaResolver.calls)
			require.Empty(t, repository.saveCalls)
		})
	}
}

func TestServiceSaveDraftBoundsAmplificationBeforeResolversAndRepository(t *testing.T) {
	t.Run("exact tag and media limits", func(t *testing.T) {
		repository := &revisionRepositoryFake{}
		tags := &tagResolverFake{}
		coverID := int64(91)
		mediaResolver := &mediaResolverFake{cover: media.Media{ID: coverID, PublicKey: firstMediaKey, State: "active"}}
		service := newRevisionService(t, repository, tags, mediaResolver, time.Now)
		tagIDs := make([]int64, MaxTagCount)
		for index := range tagIDs {
			tagIDs[index] = int64(index + 1)
		}

		_, err := service.SaveDraft(context.Background(), 11, 1, Content{
			Title: "Draft", TagIDs: tagIDs, CoverMediaID: &coverID, ContentMD: registeredImagesMarkdown(MaxBodyMediaCount, false),
		})

		require.NoError(t, err)
		require.Len(t, tags.calls, 1)
		require.Len(t, tags.calls[0], MaxTagCount)
		require.Len(t, mediaResolver.calls, 1)
		require.Equal(t, &coverID, mediaResolver.calls[0].coverID)
		require.Len(t, mediaResolver.calls[0].publicKeys, MaxBodyMediaCount)
		require.Len(t, repository.saveCalls, 1)
	})

	for _, test := range []struct {
		name    string
		content Content
	}{
		{name: "tags over limit", content: Content{Title: "Draft", TagIDs: make([]int64, MaxTagCount+1)}},
		{name: "unique body media over limit", content: Content{Title: "Draft", ContentMD: registeredImagesMarkdown(MaxBodyMediaCount+1, false)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &revisionRepositoryFake{}
			tags := &tagResolverFake{}
			mediaResolver := &mediaResolverFake{}
			service := newRevisionService(t, repository, tags, mediaResolver, time.Now)

			_, err := service.SaveDraft(context.Background(), 11, 1, test.content)

			require.ErrorIs(t, err, ErrInvalidContent)
			require.Empty(t, tags.calls)
			require.Empty(t, mediaResolver.calls)
			require.Empty(t, repository.saveCalls)
		})
	}
}

func TestServiceSaveDraftMapsResolverDomainFailuresToInvalidContent(t *testing.T) {
	for _, test := range []struct {
		name     string
		tagErr   error
		mediaErr error
	}{
		{name: "tag missing", tagErr: tag.ErrNotFound},
		{name: "tag selection", tagErr: tag.ErrInvalidSelection},
		{name: "media missing", mediaErr: media.ErrNotFound},
		{name: "media invalid", mediaErr: media.ErrInvalidMetadata},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &revisionRepositoryFake{}
			tags := &tagResolverFake{err: test.tagErr}
			mediaResolver := &mediaResolverFake{err: test.mediaErr}
			service := newRevisionService(t, repository, tags, mediaResolver, time.Now)

			_, err := service.SaveDraft(context.Background(), 11, 1, Content{Title: "Draft", TagIDs: []int64{7}})

			require.ErrorIs(t, err, ErrInvalidContent)
			require.Empty(t, repository.saveCalls)
		})
	}
}

func TestServiceSaveDraftSanitizesOperationalFailuresAndPreservesRevisionDomains(t *testing.T) {
	t.Run("tag dependency", func(t *testing.T) {
		service := newRevisionService(t, &revisionRepositoryFake{}, &tagResolverFake{err: errors.New("tag-name-secret")}, &mediaResolverFake{}, time.Now)
		_, err := service.SaveDraft(context.Background(), 11, 1, Content{Title: "Draft"})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "tag-name-secret")
	})

	t.Run("media dependency", func(t *testing.T) {
		service := newRevisionService(t, &revisionRepositoryFake{}, &tagResolverFake{}, &mediaResolverFake{err: media.ErrDependencyUnavailable}, time.Now)
		_, err := service.SaveDraft(context.Background(), 11, 1, Content{Title: "Draft"})
		require.Error(t, err)
		require.False(t, errors.Is(err, ErrInvalidContent))
	})

	for _, domain := range []error{ErrConflict, ErrArticleInactive, ErrNotFound} {
		repository := &revisionRepositoryFake{saveErr: domain}
		service := newRevisionService(t, repository, &tagResolverFake{}, &mediaResolverFake{}, time.Now)
		_, err := service.SaveDraft(context.Background(), 11, 1, Content{Title: "Draft"})
		require.ErrorIs(t, err, domain)
	}

	repository := &revisionRepositoryFake{saveErr: errors.New("markdown-body-secret")}
	service := newRevisionService(t, repository, &tagResolverFake{}, &mediaResolverFake{}, time.Now)
	_, err := service.SaveDraft(context.Background(), 11, 1, Content{Title: "Draft"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "markdown-body-secret")
}

func TestServiceGetDraftAndPreviewReadCompleteCurrentDraftWithoutMutation(t *testing.T) {
	want := Draft{ID: 21, ArticleID: 11, RevisionNo: 2, LockVersion: 3, Status: StatusEditing, Reason: ReasonDraft}
	repository := &revisionRepositoryFake{draft: want}
	service := newRevisionService(t, repository, &tagResolverFake{}, &mediaResolverFake{}, time.Now)

	got, err := service.GetDraft(context.Background(), 11)
	require.NoError(t, err)
	require.Equal(t, want, got)
	preview, err := service.Preview(context.Background(), 11)
	require.NoError(t, err)
	require.Equal(t, want, preview)
	require.Equal(t, []int64{11, 11}, repository.getCalls)
	require.Empty(t, repository.saveCalls)
}

func TestServiceValidateFreezableUsesStoredDraftContent(t *testing.T) {
	service := newRevisionService(t, &revisionRepositoryFake{}, &tagResolverFake{}, &mediaResolverFake{}, time.Now)
	require.NoError(t, service.ValidateFreezable(Draft{Title: "Version", ContentMD: "body"}))
	require.ErrorIs(t, service.ValidateFreezable(Draft{Title: "Version", ContentMD: "![x](blob:https://admin/x)"}), ErrInvalidContent)
	require.ErrorIs(t, service.ValidateFreezable(Draft{Title: "  ", ContentMD: "body"}), ErrInvalidContent)
}

func TestNewServiceRejectsNilDependenciesAndMethodsAreNilSafe(t *testing.T) {
	repository := &revisionRepositoryFake{}
	tags := &tagResolverFake{}
	mediaResolver := &mediaResolverFake{}
	var typedNilRepository *revisionRepositoryFake
	var typedNilTags *tagResolverFake
	var typedNilMedia *mediaResolverFake
	for _, test := range []struct {
		name       string
		repository Repository
		tags       TagResolver
		media      MediaResolver
		now        func() time.Time
	}{
		{name: "nil repository", tags: tags, media: mediaResolver, now: time.Now},
		{name: "typed nil repository", repository: typedNilRepository, tags: tags, media: mediaResolver, now: time.Now},
		{name: "nil tags", repository: repository, media: mediaResolver, now: time.Now},
		{name: "typed nil tags", repository: repository, tags: typedNilTags, media: mediaResolver, now: time.Now},
		{name: "nil media", repository: repository, tags: tags, now: time.Now},
		{name: "typed nil media", repository: repository, tags: tags, media: typedNilMedia, now: time.Now},
		{name: "nil clock", repository: repository, tags: tags, media: mediaResolver},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewService(test.repository, test.tags, test.media, test.now)
			require.Nil(t, service)
			require.Error(t, err)
		})
	}

	var nilService *service
	require.NotPanics(t, func() {
		_, err := nilService.GetDraft(context.Background(), 11)
		require.Error(t, err)
	})
	valid := newRevisionService(t, repository, tags, mediaResolver, time.Now)
	_, err := valid.GetDraft(nil, 11)
	require.Error(t, err)
}

type revisionSaveCall struct {
	articleID   int64
	lockVersion int64
	content     PreparedContent
	at          time.Time
}

type revisionRepositoryFake struct {
	draft      Draft
	getErr     error
	getCalls   []int64
	saveResult Draft
	saveErr    error
	saveCalls  []revisionSaveCall
	order      *[]string
}

func (r *revisionRepositoryFake) GetDraft(_ context.Context, articleID int64) (Draft, error) {
	r.getCalls = append(r.getCalls, articleID)
	return r.draft, r.getErr
}

func (r *revisionRepositoryFake) SaveDraft(_ context.Context, articleID, lockVersion int64, content PreparedContent, at time.Time) (Draft, error) {
	if r.order != nil {
		*r.order = append(*r.order, "repository")
	}
	r.saveCalls = append(r.saveCalls, revisionSaveCall{articleID: articleID, lockVersion: lockVersion, content: content, at: at})
	return r.saveResult, r.saveErr
}

type tagResolverFake struct {
	snapshots []tag.Snapshot
	err       error
	calls     [][]int64
	order     *[]string
}

func (r *tagResolverFake) Snapshots(_ context.Context, ids []int64) ([]tag.Snapshot, error) {
	if r.order != nil {
		*r.order = append(*r.order, "tags")
	}
	r.calls = append(r.calls, append([]int64(nil), ids...))
	return append([]tag.Snapshot(nil), r.snapshots...), r.err
}

type mediaResolveCall struct {
	coverID    *int64
	publicKeys []string
}

type mediaResolverFake struct {
	cover      media.Media
	references []media.Reference
	err        error
	calls      []mediaResolveCall
	order      *[]string
}

func (r *mediaResolverFake) ResolveReferences(_ context.Context, coverID *int64, publicKeys []string) (*media.Media, []media.Reference, error) {
	if r.order != nil {
		*r.order = append(*r.order, "media")
	}
	call := mediaResolveCall{publicKeys: append([]string(nil), publicKeys...)}
	if coverID != nil {
		value := *coverID
		call.coverID = &value
	}
	r.calls = append(r.calls, call)
	if r.err != nil {
		return nil, nil, r.err
	}
	var cover *media.Media
	if coverID != nil {
		value := r.cover
		cover = &value
	}
	return cover, append([]media.Reference(nil), r.references...), nil
}

func newRevisionService(t *testing.T, repository Repository, tags TagResolver, mediaResolver MediaResolver, now func() time.Time) Service {
	t.Helper()
	service, err := NewService(repository, tags, mediaResolver, now)
	require.NoError(t, err)
	return service
}
