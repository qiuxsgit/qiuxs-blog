package tag

import (
	"errors"
	"time"
)

type Tag struct {
	ID        int64
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Snapshot struct {
	TagID    int64
	Name     string
	Slug     string
	Position int
}

var (
	ErrNotFound         = errors.New("tag not found")
	ErrNameConflict     = errors.New("tag name conflict")
	ErrSlugConflict     = errors.New("tag slug conflict")
	ErrInvalidName      = errors.New("invalid tag name")
	ErrInvalidSelection = errors.New("invalid tag selection")
)
