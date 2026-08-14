package builder

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
)

const (
	jenkinsRequestTimeout   = 10 * time.Second
	maxJenkinsResponseBytes = 64 * 1024
	jenkinsQueueHeader      = "X-Jenkins-Queue-Id"
)

var errJenkinsTransportNotConfigured = errors.New("Jenkins HTTP client transport is not configured")

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		return nil, errors.New("Jenkins HTTP client is required")
	}
	if httpClient.Transport != nil && nilBuilderInterface(httpClient.Transport) {
		return nil, errJenkinsTransportNotConfigured
	}
	clone := *httpClient
	clone.Timeout = 0
	clone.Jar = nil
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{httpClient: &clone}, nil
}

func (c *Client) Test(ctx context.Context, config StoredConfig, box *platform.SecretBox) error {
	if err := c.validate(ctx, config, box); err != nil {
		return err
	}
	token, err := box.Open(config.EncryptedToken)
	if err != nil {
		return builderDependency("decrypt Jenkins token", err)
	}
	requestContext, cancel := context.WithTimeout(ctx, jenkinsRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, config.BaseURL+"/api/json", nil)
	if err != nil {
		return builderDependency("create Jenkins test request", err)
	}
	request.SetBasicAuth(config.Username, string(token))
	response, err := c.httpClient.Do(request)
	if err != nil {
		return builderDependency("send Jenkins test request", err)
	}
	status, _, err := consumeJenkinsResponse(response)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return builderDependency("test Jenkins configuration", errors.New("unexpected Jenkins response status"))
	}
	return nil
}

func (c *Client) Trigger(ctx context.Context, config StoredConfig, box *platform.SecretBox, releaseID, publishJobID int64) (int64, error) {
	if err := c.validate(ctx, config, box); err != nil {
		return 0, err
	}
	if releaseID <= 0 || publishJobID <= 0 {
		return 0, builderDomain("trigger Jenkins build", ErrInvalidConfig)
	}
	token, err := box.Open(config.EncryptedToken)
	if err != nil {
		return 0, builderDependency("decrypt Jenkins token", err)
	}
	form := url.Values{
		"RELEASE_ID":     {strconv.FormatInt(releaseID, 10)},
		"PUBLISH_JOB_ID": {strconv.FormatInt(publishJobID, 10)},
	}
	requestContext, cancel := context.WithTimeout(ctx, jenkinsRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, triggerURL(config), strings.NewReader(form.Encode()))
	if err != nil {
		return 0, builderDependency("create Jenkins trigger request", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth(config.Username, string(token))
	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, builderDependency("send Jenkins trigger request", err)
	}
	status, headers, err := consumeJenkinsResponse(response)
	if err != nil {
		return 0, err
	}
	if status != http.StatusCreated && status != http.StatusFound {
		return 0, builderDependency("trigger Jenkins build", errors.New("unexpected Jenkins response status"))
	}
	queueID, err := parseQueueID(headers.Values(jenkinsQueueHeader))
	if err != nil {
		return 0, builderDependency("read Jenkins queue ID", err)
	}
	return queueID, nil
}

func (c *Client) validate(ctx context.Context, config StoredConfig, box *platform.SecretBox) error {
	if c == nil || c.httpClient == nil {
		return errors.New("Jenkins client is not configured")
	}
	if c.httpClient.Transport != nil && nilBuilderInterface(c.httpClient.Transport) {
		return errJenkinsTransportNotConfigured
	}
	if nilBuilderInterface(ctx) {
		return builderDomain("use Jenkins client", ErrInvalidConfig)
	}
	if box == nil {
		return errors.New("Jenkins secret box is required")
	}
	if config.ID <= 0 || !config.TokenConfigured || ValidateConfig(ConfigInput{
		Name: config.Name, BaseURL: config.BaseURL, Username: config.Username, JobName: config.JobName, Enabled: config.Enabled,
	}) != nil {
		return builderDomain("validate Jenkins configuration", ErrInvalidConfig)
	}
	if !config.Enabled {
		return builderDomain("use Jenkins configuration", ErrDisabled)
	}
	if !validEncryptedToken(config.EncryptedToken) {
		return builderDependency("validate stored Jenkins token", errors.New("stored Jenkins token is invalid"))
	}
	return nil
}

func triggerURL(config StoredConfig) string {
	segments := strings.Split(config.JobName, "/")
	escaped := make([]string, len(segments))
	for index, segment := range segments {
		escaped[index] = url.PathEscape(segment)
	}
	return config.BaseURL + "/job/" + strings.Join(escaped, "/job/") + "/buildWithParameters"
}

func consumeJenkinsResponse(response *http.Response) (int, http.Header, error) {
	if response == nil || nilBuilderInterface(response.Body) {
		return 0, nil, builderDependency("read Jenkins response", errors.New("Jenkins response body is required"))
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxJenkinsResponseBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return 0, nil, builderDependency("read Jenkins response", readErr)
	}
	if closeErr != nil {
		return 0, nil, builderDependency("close Jenkins response", closeErr)
	}
	if len(body) > maxJenkinsResponseBytes {
		return 0, nil, builderDependency("read Jenkins response", errors.New("Jenkins response is too large"))
	}
	return response.StatusCode, response.Header.Clone(), nil
}

func parseQueueID(values []string) (int64, error) {
	if len(values) == 0 {
		return 0, nil
	}
	if len(values) != 1 {
		return 0, errors.New("Jenkins queue ID is invalid")
	}
	encoded := values[0]
	if len(encoded) == 0 || encoded[0] < '1' || encoded[0] > '9' {
		return 0, errors.New("Jenkins queue ID is invalid")
	}
	for index := 1; index < len(encoded); index++ {
		if encoded[index] < '0' || encoded[index] > '9' {
			return 0, errors.New("Jenkins queue ID is invalid")
		}
	}
	return strconv.ParseInt(encoded, 10, 64)
}
