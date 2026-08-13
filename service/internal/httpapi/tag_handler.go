package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
)

func (h *AdminHandler) ListTags(c *gin.Context) {
	if !h.authenticate(c) {
		return
	}
	items, err := h.tags.List(c.Request.Context())
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	views := make([]TagView, len(items))
	for index := range items {
		views[index] = tagView(items[index])
	}
	c.JSON(http.StatusOK, TagList{Items: views})
}

func (h *AdminHandler) CreateTag(c *gin.Context) {
	if !h.authenticate(c) {
		return
	}
	request, err := decodeAdminJSON[CreateTagRequest](c, c.Request, c.Writer, maxAdminJSONBodyBytes)
	if err != nil {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	created, err := h.tags.Create(c.Request.Context(), request.Name)
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusCreated, tagView(created))
}

func (h *AdminHandler) RenameTag(c *gin.Context, tagID TagId) {
	if !h.authenticate(c) {
		return
	}
	if tagID <= 0 {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	request, err := decodeAdminJSON[RenameTagRequest](c, c.Request, c.Writer, maxAdminJSONBodyBytes)
	if err != nil {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	renamed, err := h.tags.Rename(c.Request.Context(), tagID, request.Name)
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusOK, tagView(renamed))
}

func tagView(item tag.Tag) TagView {
	return TagView{Id: item.ID, Name: item.Name, Slug: item.Slug, CreatedAt: item.CreatedAt.UTC(), UpdatedAt: item.UpdatedAt.UTC()}
}
