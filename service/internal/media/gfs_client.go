package media

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	gfsHTTPTimeout     = 5 * time.Second
	gfsMaxResponseBody = 64 * 1024
)

var ErrDependencyUnavailable = errors.New("media dependency unavailable")

type Metadata struct {
	FileID      int64
	FileName    string
	ContentType string
	FileSize    int64
	Width       int
	Height      int
}

type MetadataReader interface {
	Metadata(context.Context, int64) (Metadata, error)
}

type GFSClient struct {
	baseURL url.URL
	client  *http.Client
}

func NewGFSClient(baseURL string, client *http.Client) (*GFSClient, error) {
	parsedBaseURL, err := parseCanonicalBaseURL(baseURL)
	if err != nil || client == nil || client.Timeout != gfsHTTPTimeout {
		return nil, ErrInvalidGFSConfiguration
	}
	return &GFSClient{baseURL: parsedBaseURL, client: client}, nil
}

func (c *GFSClient) Metadata(ctx context.Context, fileID int64) (Metadata, error) {
	if !c.valid() || ctx == nil || fileID <= 0 {
		return Metadata{}, ErrDependencyUnavailable
	}

	metadataURL := c.baseURL
	metadataURL.Path = "/alioss/objects/" + strconv.FormatInt(fileID, 10) + "/metadata"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL.String(), nil)
	if err != nil {
		return Metadata{}, ErrDependencyUnavailable
	}
	response, err := c.client.Do(request)
	if err != nil {
		return Metadata{}, ErrDependencyUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Body == nil {
		return Metadata{}, ErrDependencyUnavailable
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, gfsMaxResponseBody+1))
	if err != nil || len(body) > gfsMaxResponseBody {
		return Metadata{}, ErrDependencyUnavailable
	}
	var envelope struct {
		Code *int `json:"code"`
		Data *struct {
			FileID        int64  `json:"fileId"`
			FileName      string `json:"fileName"`
			FileSize      int64  `json:"fileSize"`
			ContentType   string `json:"contentType"`
			ImageMetadata struct {
				ImageWidth  string `json:"imageWidth"`
				ImageHeight string `json:"imageHeight"`
			} `json:"imageMetadata"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code == nil || *envelope.Code != 0 || envelope.Data == nil || envelope.Data.FileID != fileID {
		return Metadata{}, ErrDependencyUnavailable
	}
	width, err := positiveDecimal(envelope.Data.ImageMetadata.ImageWidth)
	if err != nil {
		return Metadata{}, ErrDependencyUnavailable
	}
	height, err := positiveDecimal(envelope.Data.ImageMetadata.ImageHeight)
	if err != nil {
		return Metadata{}, ErrDependencyUnavailable
	}
	return Metadata{
		FileID:      envelope.Data.FileID,
		FileName:    envelope.Data.FileName,
		ContentType: envelope.Data.ContentType,
		FileSize:    envelope.Data.FileSize,
		Width:       width,
		Height:      height,
	}, nil
}

func (c *GFSClient) valid() bool {
	return c != nil && c.baseURL.Scheme != "" && c.baseURL.Host != "" && c.client != nil && c.client.Timeout == gfsHTTPTimeout
}

func positiveDecimal(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, ErrDependencyUnavailable
	}
	return parsed, nil
}
