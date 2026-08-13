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

// Aggregate is the observable immutable release and its complete publish-job
// history. Jobs are ordered by created_at descending, then id descending.
type Aggregate struct {
	Release Release
	Jobs    []PublishJob
}

// LatestJob returns a copy of the first job in the repository-defined order.
// An empty history is a corrupt repository result because release creation and
// its initial job are atomic.
func (a Aggregate) LatestJob() (PublishJob, error) {
	if err := a.Validate(); err != nil {
		return PublishJob{}, ErrInvalidAggregate
	}
	return clonePublishJob(a.Jobs[0]), nil
}

// Validate enforces the repository boundary: all jobs belong to the release
// and are ordered by created_at descending, then id descending.
func (a Aggregate) Validate() error {
	if a.Release.ID <= 0 || len(a.Jobs) == 0 {
		return ErrInvalidAggregate
	}
	for index, job := range a.Jobs {
		if job.ID <= 0 || job.ReleaseID != a.Release.ID {
			return ErrInvalidAggregate
		}
		if index == 0 {
			continue
		}
		previous := a.Jobs[index-1]
		if previous.CreatedAt.Before(job.CreatedAt) ||
			(previous.CreatedAt.Equal(job.CreatedAt) && previous.ID <= job.ID) {
			return ErrInvalidAggregate
		}
	}
	return nil
}

// ValidateRetry additionally proves that a retry transaction returned the new
// job as both the latest job and the first item of the complete history.
func (a Aggregate) ValidateRetry(created PublishJob) error {
	if err := a.Validate(); err != nil || !publishJobsEqual(a.Jobs[0], created) {
		return ErrInvalidAggregate
	}
	return nil
}

func clonePublishJob(job PublishJob) PublishJob {
	clone := job
	if job.BuildNumber != nil {
		value := *job.BuildNumber
		clone.BuildNumber = &value
	}
	if job.FinishedAt != nil {
		value := *job.FinishedAt
		clone.FinishedAt = &value
	}
	return clone
}

func publishJobsEqual(left, right PublishJob) bool {
	return left.ID == right.ID &&
		left.ReleaseID == right.ReleaseID &&
		left.BuilderID == right.BuilderID &&
		left.Status == right.Status &&
		left.Stage == right.Stage &&
		optionalInt64Equal(left.BuildNumber, right.BuildNumber) &&
		left.ErrorSummary == right.ErrorSummary &&
		left.CreatedAt.Equal(right.CreatedAt) &&
		optionalTimeEqual(left.FinishedAt, right.FinishedAt)
}

func optionalInt64Equal(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func optionalTimeEqual(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}

type ListQuery struct {
	Limit  int
	Offset int
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

type SnapshotRequest struct {
	Mode             PublishMode
	ArticleID        int64
	CurrentReleaseID int64
	Base             PreparedSnapshot
}

type PreparedSnapshot struct {
	Site     SiteSnapshot
	Articles []ArticleSnapshot
	Checksum string
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
	ErrInvalidAggregate       = errors.New("release aggregate is invalid")
	ErrInvalidSnapshot        = errors.New("release snapshot is invalid")
	ErrDependencyUnavailable  = errors.New("release dependency unavailable")
)
