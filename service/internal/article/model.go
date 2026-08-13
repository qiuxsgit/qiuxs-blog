package article

import (
	"errors"
	"time"
)

type State string

const (
	StateActive  State = "active"
	StateTrashed State = "trashed"
)

type Article struct {
	ID                  int64
	Slug                string
	DraftRevisionID     int64
	PublishedRevisionID *int64
	State               State
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type Summary struct {
	Article
	DraftTitle     string
	DraftUpdatedAt time.Time
}

var (
	ErrNotFound          = errors.New("article not found")
	ErrSlugConflict      = errors.New("article slug conflict")
	ErrMustBeUnpublished = errors.New("article must be unpublished before trash")
	ErrStateConflict     = errors.New("article state conflict")
)
