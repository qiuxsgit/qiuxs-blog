package release

import (
	"errors"
	"time"
)

type ReleaseStatus string

const (
	ReleaseQueued  ReleaseStatus = "queued"
	ReleaseSuccess ReleaseStatus = "success"
	ReleaseFailed  ReleaseStatus = "failed"
)

type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobQueued    JobStatus = "queued"
	JobBuilding  JobStatus = "building"
	JobDeploying JobStatus = "deploying"
	JobSuccess   JobStatus = "success"
	JobFailed    JobStatus = "failed"
)

type PublishMode string

const (
	PublishArticle   PublishMode = "publish_article"
	UnpublishArticle PublishMode = "unpublish_article"
	PublishSettings  PublishMode = "publish_settings"
)

type SocialLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type SiteSnapshot struct {
	Name          string       `json:"name"`
	AuthorBio     string       `json:"authorBio"`
	AboutMarkdown string       `json:"aboutMarkdown"`
	FilingName    string       `json:"filingName"`
	FilingNumber  string       `json:"filingNumber"`
	SocialLinks   []SocialLink `json:"socialLinks"`
}

type TagSnapshot struct {
	ID   int64
	Name string
	Slug string
}

type ArticleSnapshot struct {
	ArticleID       int64
	RevisionID      int64
	Slug            string
	Title           string
	Summary         string
	ContentMarkdown string
	ContentHash     string
	PublishedAt     time.Time
	Tags            []TagSnapshot
}

type Release struct {
	ID          int64
	Status      ReleaseStatus
	Site        SiteSnapshot
	Checksum    string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

type PublishJob struct {
	ID           int64
	ReleaseID    int64
	BuilderID    int64
	Status       JobStatus
	Stage        string
	BuildNumber  *int64
	ErrorSummary string
	CreatedAt    time.Time
	FinishedAt   *time.Time
}

type Bundle struct {
	SchemaVersion int             `json:"schemaVersion"`
	ReleaseID     int64           `json:"releaseId"`
	GeneratedAt   time.Time       `json:"generatedAt"`
	Site          BundleSite      `json:"site"`
	Tags          []BundleTag     `json:"tags"`
	Articles      []BundleArticle `json:"articles"`
	Checksum      string          `json:"checksum"`
}

type BundleSite struct {
	Name          string       `json:"name"`
	AuthorBio     string       `json:"authorBio"`
	AboutMarkdown string       `json:"aboutMarkdown"`
	FilingName    string       `json:"filingName"`
	FilingNumber  string       `json:"filingNumber"`
	SocialLinks   []SocialLink `json:"socialLinks"`
}

type BundleTag struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type BundleArticle struct {
	ArticleID       int64     `json:"articleId"`
	RevisionID      int64     `json:"revisionId"`
	Slug            string    `json:"slug"`
	Title           string    `json:"title"`
	Summary         string    `json:"summary"`
	ContentMarkdown string    `json:"contentMarkdown"`
	ContentHash     string    `json:"contentHash"`
	PublishedAt     time.Time `json:"publishedAt"`
	Tags            []string  `json:"tags"`
}

type CreateCommand struct {
	Mode        PublishMode
	ArticleID   int64
	BuilderID   int64
	RequestedBy int64
}

type CallbackEvent struct {
	ReleaseID    int64
	BuildNumber  int64
	Stage        string
	Status       JobStatus
	ErrorSummary string
	Timestamp    time.Time
	Nonce        string
}

type Artifact struct {
	ReleaseID   int64     `json:"releaseId"`
	Checksum    string    `json:"checksum"`
	BuildNumber int64     `json:"buildNumber"`
	DeployedAt  time.Time `json:"deployedAt"`
}

var (
	ErrBusy                   = errors.New("publish already active")
	ErrNotFound               = errors.New("release not found")
	ErrConflict               = errors.New("invalid publish transition")
	ErrReconciliationRequired = errors.New("release reconciliation required")
)
