package revision

import (
	"context"
	"errors"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/tag"
)

type Status string
type Reason string

const (
	StatusEditing Status = "editing"
	StatusFrozen  Status = "frozen"

	ReasonDraft           Reason = "draft"
	ReasonManualVersion   Reason = "manual_version"
	ReasonPublishSnapshot Reason = "publish_snapshot"
)

type Content struct {
	Title        string
	Summary      string
	CoverMediaID *int64
	ContentMD    string
	TagIDs       []int64
}

type Draft struct {
	ID           int64
	ArticleID    int64
	RevisionNo   int64
	LockVersion  int64
	Status       Status
	Reason       Reason
	Title        string
	Summary      string
	CoverMediaID *int64
	ContentMD    string
	ContentHash  string
	Tags         []tag.Snapshot
	Media        []media.Reference
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Version struct {
	Draft
}

type PreparedContent struct {
	Title       string
	Summary     string
	Cover       *media.Media
	ContentMD   string
	Tags        []tag.Snapshot
	Media       []media.Reference
	ContentHash string
}

type DraftReader interface {
	GetDraft(context.Context, int64) (Draft, error)
}

var (
	ErrNotFound        = errors.New("revision not found")
	ErrConflict        = errors.New("revision optimistic lock conflict")
	ErrInvalidContent  = errors.New("revision content is invalid")
	ErrNotFrozen       = errors.New("revision is not frozen")
	ErrArticleInactive = errors.New("article is not active")
)
