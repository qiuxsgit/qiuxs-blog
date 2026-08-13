package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
)

func (h *AdminHandler) CreateMediaUploadPolicy(c *gin.Context) {
	if !h.authenticate(c) {
		return
	}
	policy, err := h.media.IssueUploadPolicy(c.Request.Context())
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusOK, mediaUploadPolicyView(policy))
}

func (h *AdminHandler) RegisterMedia(c *gin.Context) {
	if !h.authenticate(c) {
		return
	}
	request, err := decodeAdminJSON[RegisterMediaRequest](c, c.Request, c.Writer, maxAdminJSONBodyBytes)
	if err != nil || request.GfsFileId <= 0 {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	registered, err := h.media.Register(c.Request.Context(), request.GfsFileId, request.OriginalName)
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusCreated, mediaView(registered))
}

func mediaUploadPolicyView(policy media.UploadPolicy) MediaUploadPolicy {
	return MediaUploadPolicy{
		UploadUrl: policy.UploadURL, AppId: policy.AppID, Policy: policy.Policy, Signature: policy.Signature,
		Timestamp: policy.Timestamp, Expire: policy.Expire, Nonce: policy.Nonce, FileField: policy.FileField,
	}
}

func mediaView(item media.Media) MediaView {
	return MediaView{
		Id: item.ID, PublicKey: item.PublicKey, GfsFileId: item.GFSFileID, OriginalName: item.OriginalName,
		MimeType: MediaViewMimeType(item.MIMEType), FileSize: item.FileSize, Width: item.Width, Height: item.Height,
		State: MediaViewState(item.State), Url: "/img/proxy/" + item.PublicKey,
		CreatedAt: item.CreatedAt.UTC(), UpdatedAt: item.UpdatedAt.UTC(),
	}
}
