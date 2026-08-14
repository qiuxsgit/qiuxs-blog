package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/auth"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/builder"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/release"
)

var releaseETagPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type builderTester interface {
	Test(context.Context, builder.StoredConfig, *platform.SecretBox) error
}

type releaseReader interface {
	FindRelease(context.Context, int64) (release.Aggregate, error)
	ListReleases(context.Context, release.ListQuery) ([]release.Aggregate, error)
}

type releaseBundler interface {
	Bundle(context.Context, int64) ([]byte, string, error)
}

type releaseOperations interface {
	Publish(context.Context, release.CreateCommand) (release.Release, release.PublishJob, error)
	Retry(context.Context, int64) (release.Aggregate, release.PublishJob, error)
	Callback(context.Context, release.CallbackEvent, bool) (release.PublishJob, bool, error)
}

// ReleaseHandler implements the Stage 3 generated operations without exposing
// encrypted builder material or coupling Internal routes to Admin sessions.
type ReleaseHandler struct {
	configs    builder.ConfigRepository
	tester     builderTester
	box        *platform.SecretBox
	reader     releaseReader
	bundler    releaseBundler
	operations releaseOperations
}

func NewReleaseHandler(configs builder.ConfigRepository, tester builderTester, box *platform.SecretBox, reader releaseReader, bundler releaseBundler, operations releaseOperations) (*ReleaseHandler, error) {
	switch {
	case nilAdminDependency(configs):
		return nil, errors.New("builder configuration repository is required")
	case nilAdminDependency(tester):
		return nil, errors.New("Jenkins tester is required")
	case box == nil:
		return nil, errors.New("builder secret box is required")
	case nilAdminDependency(reader):
		return nil, errors.New("release reader is required")
	case nilAdminDependency(bundler):
		return nil, errors.New("release bundler is required")
	case nilAdminDependency(operations):
		return nil, errors.New("release orchestrator is required")
	}
	return &ReleaseHandler{configs: configs, tester: tester, box: box, reader: reader, bundler: bundler, operations: operations}, nil
}

func (h *ReleaseHandler) GetBuilderConfig(c *gin.Context) {
	if _, ok := h.admin(c, false); !ok {
		return
	}
	stored, err := h.configs.Load(c.Request.Context())
	if err != nil {
		writeReleaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, builderConfigView(stored.ConfigView))
}

func (h *ReleaseHandler) PutBuilderConfig(c *gin.Context) {
	if _, ok := h.admin(c, false); !ok {
		return
	}
	request, err := decodeAdminJSON[PutBuilderConfigRequest](c, c.Request, c.Writer, maxAdminJSONBodyBytes)
	if err != nil || request.Token != nil && *request.Token == "" {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	input := builder.ConfigInput{
		Name: request.Name, BaseURL: request.BaseUrl, Username: request.Username,
		JobName: request.JobName, Enabled: request.Enabled,
	}
	if request.Token != nil {
		input.Token = *request.Token
	}
	stored, err := h.configs.Save(c.Request.Context(), input)
	if err != nil {
		writeReleaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, builderConfigView(stored))
}

func (h *ReleaseHandler) TestBuilderConfig(c *gin.Context) {
	if _, ok := h.admin(c, false); !ok {
		return
	}
	stored, err := h.configs.Load(c.Request.Context())
	if errors.Is(err, builder.ErrNotFound) {
		WriteProblem(c, ErrPreconditionFailed)
		return
	}
	if err == nil {
		err = h.tester.Test(c.Request.Context(), stored, h.box)
	}
	if err != nil {
		writeReleaseProblem(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ReleaseHandler) ListReleases(c *gin.Context, params ListReleasesParams) {
	if _, ok := h.admin(c, true); !ok {
		return
	}
	query, ok := releaseListQuery(c, params)
	if !ok {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	items, err := h.reader.ListReleases(c.Request.Context(), query)
	if err != nil {
		writeReleaseProblem(c, err)
		return
	}
	views := make([]ReleaseView, len(items))
	for index := range items {
		views[index], err = releaseAggregateView(items[index])
		if err != nil {
			WriteProblem(c, ErrDependencyUnavailable)
			return
		}
	}
	c.JSON(http.StatusOK, ReleaseList{Items: views})
}

func (h *ReleaseHandler) CreateRelease(c *gin.Context) {
	admin, ok := h.admin(c, false)
	if !ok {
		return
	}
	request, err := decodeAdminJSON[CreateReleaseRequest](c, c.Request, c.Writer, maxAdminJSONBodyBytes)
	if err != nil {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	command, ok := createReleaseCommand(request, admin.ID)
	if !ok {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	created, job, err := h.operations.Publish(c.Request.Context(), command)
	if err != nil {
		writeReleaseProblem(c, err)
		return
	}
	view, err := releaseAggregateView(release.Aggregate{Release: created, Jobs: []release.PublishJob{job}})
	if err != nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	setReleaseJobLogContext(c, created.ID, job)
	c.JSON(http.StatusAccepted, CreateReleaseResult{Release: view, Job: publishJobView(job)})
}

func (h *ReleaseHandler) GetRelease(c *gin.Context, releaseID ReleaseId) {
	if _, ok := h.admin(c, false); !ok {
		return
	}
	if releaseID <= 0 {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	setReleaseLogContext(c, releaseID, 0, 0, "")
	aggregate, err := h.reader.FindRelease(c.Request.Context(), releaseID)
	if err != nil {
		writeReleaseProblem(c, err)
		return
	}
	view, err := releaseAggregateView(aggregate)
	if err != nil || aggregate.Release.ID != releaseID {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	latest, err := aggregate.LatestJob()
	if err != nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	setReleaseJobLogContext(c, aggregate.Release.ID, latest)
	c.JSON(http.StatusOK, view)
}

func (h *ReleaseHandler) RetryRelease(c *gin.Context, releaseID ReleaseId) {
	if _, ok := h.admin(c, false); !ok {
		return
	}
	if releaseID <= 0 {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	setReleaseLogContext(c, releaseID, 0, 0, "")
	aggregate, job, err := h.operations.Retry(c.Request.Context(), releaseID)
	if err != nil {
		writeReleaseProblem(c, err)
		return
	}
	view, err := releaseAggregateView(aggregate)
	if err != nil || aggregate.Release.ID != releaseID || view.LatestJob != publishJobView(job) {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	setReleaseJobLogContext(c, aggregate.Release.ID, job)
	c.JSON(http.StatusAccepted, RetryReleaseResult{Release: view, Job: publishJobView(job)})
}

func (h *ReleaseHandler) AcceptJenkinsCallback(c *gin.Context) {
	if h == nil || nilAdminDependency(h.operations) || c == nil || c.Request == nil || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	payload, verifierDuplicate, ok := JenkinsCallbackFrom(c)
	if !ok {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	setReleaseLogContext(c, payload.ReleaseID, payload.PublishJobID, payload.BuildNumber, payload.Status)
	if _, _, err := h.operations.Callback(c.Request.Context(), payload.Event(), verifierDuplicate); err != nil {
		writeReleaseProblem(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ReleaseHandler) GetReleaseBundle(c *gin.Context, releaseID ReleaseId) {
	if h == nil || nilAdminDependency(h.bundler) || c == nil || c.Request == nil || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || releaseID <= 0 {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	if bundleTokenAuthenticated(c) {
		setReleaseLogContext(c, releaseID, 0, 0, "")
	}
	c.Header("Vary", "Accept-Encoding")
	encoding := selectBundleEncoding(c.Request.Header.Values("Accept-Encoding"))
	if encoding == bundleEncodingNotAcceptable {
		WriteProblem(c, ErrNotAcceptable)
		return
	}
	body, etag, err := h.bundler.Bundle(c.Request.Context(), releaseID)
	if err != nil {
		writeReleaseProblem(c, err)
		return
	}
	if len(body) == 0 || !releaseETagPattern.MatchString(etag) {
		WriteProblem(c, ErrDependencyUnavailable)
		return
	}
	encoded := body
	if encoding == bundleEncodingGzip {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(body); err != nil || writer.Close() != nil {
			WriteProblem(c, ErrDependencyUnavailable)
			return
		}
		encoded = compressed.Bytes()
		c.Header("Content-Encoding", "gzip")
	}
	c.Header("Content-Type", "application/json")
	c.Header("ETag", `"`+etag+`"`)
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Length", strconv.Itoa(len(encoded)))
	c.Data(http.StatusOK, "application/json", encoded)
}

func setReleaseJobLogContext(c *gin.Context, releaseID int64, job release.PublishJob) {
	buildNumber := int64(0)
	if job.BuildNumber != nil {
		buildNumber = *job.BuildNumber
	}
	setReleaseLogContext(c, releaseID, job.ID, buildNumber, job.Status)
}

func (h *ReleaseHandler) admin(c *gin.Context, allowQuery bool) (admin auth.Admin, ok bool) {
	if h == nil || nilAdminDependency(h.configs) || nilAdminDependency(h.reader) || nilAdminDependency(h.bundler) || nilAdminDependency(h.operations) || nilAdminDependency(h.tester) || h.box == nil {
		WriteProblem(c, ErrDependencyUnavailable)
		return admin, false
	}
	admin, ok = requireAdmin(c)
	if !ok {
		return admin, false
	}
	setAdminLogContext(c, admin.ID)
	if !allowQuery && (c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery) {
		WriteProblem(c, ErrInvalidRequest)
		return admin, false
	}
	return admin, true
}

func builderConfigView(value builder.ConfigView) BuilderConfigView {
	return BuilderConfigView{Id: value.ID, Name: value.Name, BaseUrl: value.BaseURL, Username: value.Username, JobName: value.JobName, Enabled: value.Enabled, TokenConfigured: value.TokenConfigured}
}

func createReleaseCommand(request CreateReleaseRequest, adminID int64) (release.CreateCommand, bool) {
	command := release.CreateCommand{Mode: release.PublishMode(request.Mode), RequestedBy: adminID}
	switch command.Mode {
	case release.PublishSettings:
		return command, request.ArticleId == nil && adminID > 0
	case release.PublishArticle, release.UnpublishArticle:
		if request.ArticleId == nil || *request.ArticleId <= 0 || adminID <= 0 {
			return release.CreateCommand{}, false
		}
		command.ArticleID = *request.ArticleId
		return command, true
	default:
		return release.CreateCommand{}, false
	}
}

func releaseListQuery(c *gin.Context, params ListReleasesParams) (release.ListQuery, bool) {
	if c.Request.URL.ForceQuery {
		return release.ListQuery{}, false
	}
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return release.ListQuery{}, false
	}
	for key, entries := range values {
		if (key != "limit" && key != "offset") || len(entries) != 1 {
			return release.ListQuery{}, false
		}
	}
	query := release.ListQuery{Limit: 20}
	if params.Limit != nil {
		query.Limit = *params.Limit
	}
	if params.Offset != nil {
		query.Offset = *params.Offset
	}
	return query, query.Limit >= 1 && query.Limit <= 100 && query.Offset >= 0
}

func releaseAggregateView(aggregate release.Aggregate) (ReleaseView, error) {
	latest, err := aggregate.LatestJob()
	if err != nil {
		return ReleaseView{}, err
	}
	jobs := make([]PublishJobView, len(aggregate.Jobs))
	for index := range aggregate.Jobs {
		jobs[index] = publishJobView(aggregate.Jobs[index])
	}
	item := aggregate.Release
	return ReleaseView{
		Id: item.ID, Status: ReleaseViewStatus(item.Status), Checksum: item.Checksum,
		CreatedAt: item.CreatedAt.UTC(), CompletedAt: utcTimePointer(item.CompletedAt),
		LatestJob: publishJobView(latest), Jobs: jobs,
	}, nil
}

func publishJobView(job release.PublishJob) PublishJobView {
	return PublishJobView{
		Id: job.ID, ReleaseId: job.ReleaseID, BuilderId: job.BuilderID,
		BuilderTarget: BuilderTargetView{
			Name: job.BuilderTarget.Name, BaseUrl: job.BuilderTarget.BaseURL,
			Username: job.BuilderTarget.Username, JobName: job.BuilderTarget.JobName,
		},
		Status: PublishJobViewStatus(job.Status), Stage: job.Stage,
		BuildNumber: copyInt64Pointer(job.BuildNumber), ErrorSummary: job.ErrorSummary,
		CreatedAt: job.CreatedAt.UTC(), FinishedAt: utcTimePointer(job.FinishedAt),
	}
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

type bundleEncoding uint8

const (
	bundleEncodingNotAcceptable bundleEncoding = iota
	bundleEncodingIdentity
	bundleEncodingGzip
)

type encodingQuality struct {
	value int
	set   bool
}

func selectBundleEncoding(values []string) bundleEncoding {
	qualities := map[string]encodingQuality{}
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			parts := strings.Split(strings.TrimSpace(item), ";")
			coding := strings.ToLower(strings.TrimSpace(parts[0]))
			if coding == "" {
				continue
			}
			quality := 1000
			valid := true
			qualitySeen := false
			for _, parameter := range parts[1:] {
				name, encoded, found := strings.Cut(strings.TrimSpace(parameter), "=")
				if !found || !strings.EqualFold(strings.TrimSpace(name), "q") || qualitySeen {
					valid = false
					break
				}
				qualitySeen = true
				parsed, ok := parseEncodingQuality(strings.TrimSpace(encoded))
				if !ok {
					valid = false
					break
				}
				quality = parsed
			}
			if !valid {
				continue
			}
			current := qualities[coding]
			if !current.set || quality > current.value {
				qualities[coding] = encodingQuality{value: quality, set: true}
			}
		}
	}

	wildcard := qualities["*"]
	gzip := qualities["gzip"]
	if !gzip.set && wildcard.set {
		gzip = wildcard
	}
	identity := qualities["identity"]
	if !identity.set {
		identity = encodingQuality{value: 1000, set: true}
		if wildcard.set && wildcard.value == 0 {
			identity.value = 0
		}
	}
	if gzip.value > 0 && gzip.value >= identity.value {
		return bundleEncodingGzip
	}
	if identity.value > 0 {
		return bundleEncodingIdentity
	}
	return bundleEncodingNotAcceptable
}

func parseEncodingQuality(value string) (int, bool) {
	if value == "0" || value == "1" {
		return int(value[0]-'0') * 1000, true
	}
	if len(value) < 2 || len(value) > 5 || value[1] != '.' || (value[0] != '0' && value[0] != '1') {
		return 0, false
	}
	fraction := value[2:]
	quality := 0
	for _, digit := range fraction {
		if digit < '0' || digit > '9' || value[0] == '1' && digit != '0' {
			return 0, false
		}
		quality = quality*10 + int(digit-'0')
	}
	for range 3 - len(fraction) {
		quality *= 10
	}
	if value[0] == '1' {
		return 1000, true
	}
	return quality, true
}

func writeReleaseProblem(c *gin.Context, err error) {
	if knownReleaseError(err) {
		WriteProblem(c, err)
		return
	}
	WriteProblem(c, ErrDependencyUnavailable)
}

func knownReleaseError(err error) bool {
	return errors.Is(err, builder.ErrInvalidConfig) || errors.Is(err, builder.ErrNotFound) ||
		errors.Is(err, builder.ErrConflict) || errors.Is(err, builder.ErrDisabled) ||
		errors.Is(err, builder.ErrDependencyUnavailable) || errors.Is(err, release.ErrBusy) ||
		errors.Is(err, release.ErrNotFound) || errors.Is(err, release.ErrConflict) ||
		errors.Is(err, release.ErrReconciliationRequired) || errors.Is(err, release.ErrInvalidSnapshot) ||
		errors.Is(err, release.ErrDependencyUnavailable)
}
