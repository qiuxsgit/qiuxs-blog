package httpapi

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/article"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/revision"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
)

func (h *AdminHandler) ListArticles(c *gin.Context, params ListArticlesParams) {
	if !h.authenticateAllowQuery(c) {
		return
	}
	state := article.StateActive
	if params.State != nil {
		if !params.State.Valid() {
			WriteProblem(c, ErrInvalidRequest)
			return
		}
		state = article.State(*params.State)
	}
	query, queryErr := url.ParseQuery(c.Request.URL.RawQuery)
	if queryErr != nil || c.Request.URL.ForceQuery || len(query) > 0 && (len(query) != 1 || len(query["state"]) != 1) {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	items, err := h.articles.List(c.Request.Context(), state)
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	views := make([]ArticleSummary, len(items))
	for index := range items {
		views[index] = articleSummaryView(items[index])
	}
	c.JSON(http.StatusOK, ArticleList{Items: views})
}

func (h *AdminHandler) CreateArticle(c *gin.Context) {
	if !h.authenticate(c) {
		return
	}
	detail, err := h.articles.Create(c.Request.Context())
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	setArticleLogContext(c, detail.Article.ID)
	c.JSON(http.StatusCreated, articleDetailView(detail))
}

func (h *AdminHandler) GetArticle(c *gin.Context, articleID ArticleId) {
	if !h.authenticateArticle(c, articleID) {
		return
	}
	detail, err := h.articles.Get(c.Request.Context(), articleID)
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusOK, articleDetailView(detail))
}

func (h *AdminHandler) SaveArticleDraft(c *gin.Context, articleID ArticleId) {
	if !h.authenticateArticle(c, articleID) {
		return
	}
	request, err := decodeAdminJSON[SaveDraftRequest](c, c.Request, c.Writer, maxAdminMarkdownBodyBytes)
	if err != nil || request.LockVersion <= 0 || len(request.TagIds) > revision.MaxTagCount {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	for _, tagID := range request.TagIds {
		if tagID <= 0 {
			WriteProblem(c, ErrInvalidRequest)
			return
		}
	}
	draft, err := h.revisions.SaveDraft(c.Request.Context(), articleID, request.LockVersion, revision.Content{
		Title: request.Title, Summary: request.Summary, CoverMediaID: copyInt64Pointer(request.CoverMediaId),
		ContentMD: request.ContentMd, TagIDs: append([]int64(nil), request.TagIds...),
	})
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusOK, draftView(draft))
}

func (h *AdminHandler) GetArticlePreview(c *gin.Context, articleID ArticleId) {
	if !h.authenticateArticle(c, articleID) {
		return
	}
	draft, err := h.revisions.Preview(c.Request.Context(), articleID)
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusOK, PreviewView{Draft: draftView(draft)})
}

func (h *AdminHandler) ListArticleVersions(c *gin.Context, articleID ArticleId) {
	if !h.authenticateArticle(c, articleID) {
		return
	}
	versions, err := h.revisions.ListVersions(c.Request.Context(), articleID)
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	items := make([]RevisionView, len(versions))
	for index := range versions {
		items[index] = revisionView(versions[index].Draft)
	}
	c.JSON(http.StatusOK, RevisionList{Items: items})
}

func (h *AdminHandler) CreateArticleVersion(c *gin.Context, articleID ArticleId) {
	if !h.authenticateArticle(c, articleID) {
		return
	}
	request, err := decodeAdminJSON[LockVersionRequest](c, c.Request, c.Writer, maxAdminJSONBodyBytes)
	if err != nil || request.LockVersion <= 0 {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	version, draft, err := h.revisions.CreateVersion(c.Request.Context(), articleID, request.LockVersion)
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusCreated, VersionResult{Version: revisionView(version.Draft), Draft: draftView(draft)})
}

func (h *AdminHandler) RestoreArticleVersion(c *gin.Context, articleID ArticleId, revisionID RevisionId) {
	if !h.authenticateArticle(c, articleID) {
		return
	}
	if revisionID <= 0 {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	request, err := decodeAdminJSON[LockVersionRequest](c, c.Request, c.Writer, maxAdminJSONBodyBytes)
	if err != nil || request.LockVersion <= 0 {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	draft, err := h.revisions.RestoreVersion(c.Request.Context(), articleID, revisionID, request.LockVersion)
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusOK, draftView(draft))
}

func (h *AdminHandler) TrashArticle(c *gin.Context, articleID ArticleId) {
	if !h.authenticateArticle(c, articleID) {
		return
	}
	if err := h.articles.Trash(c.Request.Context(), articleID); err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AdminHandler) UntrashArticle(c *gin.Context, articleID ArticleId) {
	if !h.authenticateArticle(c, articleID) {
		return
	}
	if err := h.articles.Untrash(c.Request.Context(), articleID); err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AdminHandler) authenticateArticle(c *gin.Context, articleID int64) bool {
	if !h.authenticate(c) {
		return false
	}
	if articleID <= 0 {
		WriteProblem(c, ErrInvalidRequest)
		return false
	}
	setArticleLogContext(c, articleID)
	return true
}

func articleSummaryView(item article.Summary) ArticleSummary {
	return ArticleSummary{
		Id: item.ID, Slug: item.Slug, DraftRevisionId: item.DraftRevisionID,
		PublishedRevisionId: copyInt64Pointer(item.PublishedRevisionID), State: ArticleSummaryState(item.State),
		DraftTitle: item.DraftTitle, DraftUpdatedAt: item.DraftUpdatedAt.UTC(), CreatedAt: item.CreatedAt.UTC(), UpdatedAt: item.UpdatedAt.UTC(),
	}
}

func articleDetailView(detail article.Detail) ArticleDetail {
	item := detail.Article
	return ArticleDetail{
		Id: item.ID, Slug: item.Slug, DraftRevisionId: item.DraftRevisionID,
		PublishedRevisionId: copyInt64Pointer(item.PublishedRevisionID), State: ArticleDetailState(item.State),
		CreatedAt: item.CreatedAt.UTC(), UpdatedAt: item.UpdatedAt.UTC(), Draft: draftView(detail.Draft),
	}
}

func draftView(draft revision.Draft) DraftView {
	return DraftView{
		Id: draft.ID, ArticleId: draft.ArticleID, RevisionNo: draft.RevisionNo, LockVersion: draft.LockVersion,
		Status: DraftViewStatus(draft.Status), Reason: DraftViewReason(draft.Reason), Title: draft.Title,
		Summary: draft.Summary, CoverMediaId: copyInt64Pointer(draft.CoverMediaID), ContentMd: draft.ContentMD,
		ContentHash: draft.ContentHash, Tags: tagSnapshotViews(draft.Tags), Media: mediaReferenceViews(draft.Media),
		CreatedAt: draft.CreatedAt.UTC(), UpdatedAt: draft.UpdatedAt.UTC(),
	}
}

func revisionView(draft revision.Draft) RevisionView {
	return RevisionView{
		Id: draft.ID, ArticleId: draft.ArticleID, RevisionNo: draft.RevisionNo, LockVersion: draft.LockVersion,
		Status: RevisionViewStatus(draft.Status), Reason: RevisionViewReason(draft.Reason), Title: draft.Title,
		Summary: draft.Summary, CoverMediaId: copyInt64Pointer(draft.CoverMediaID), ContentMd: draft.ContentMD,
		ContentHash: draft.ContentHash, Tags: tagSnapshotViews(draft.Tags), Media: mediaReferenceViews(draft.Media),
		CreatedAt: draft.CreatedAt.UTC(), UpdatedAt: draft.UpdatedAt.UTC(),
	}
}

func tagSnapshotViews(items []tag.Snapshot) []TagSnapshot {
	views := make([]TagSnapshot, len(items))
	for index, item := range items {
		views[index] = TagSnapshot{TagId: item.TagID, Name: item.Name, Slug: item.Slug, Position: item.Position}
	}
	return views
}

func mediaReferenceViews(items []media.Reference) []MediaReference {
	views := make([]MediaReference, len(items))
	for index, item := range items {
		views[index] = MediaReference{MediaId: item.MediaID, PublicKey: item.PublicKey, Purpose: MediaReferencePurpose(item.Purpose), Position: item.Position}
	}
	return views
}

func copyInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
