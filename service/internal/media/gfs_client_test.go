package media_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/stretchr/testify/require"
)

const validMetadataEnvelope = `{"code":0,"msg":"ok","data":{"fileId":41,"fileName":"photo.png","fileSize":8192,"contentType":"image/png","imageMetadata":{"imageWidth":"640","imageHeight":"480"}}}`

func TestGFSClientMetadataUsesExactEndpointAndMapsActualMetadata(t *testing.T) {
	requestSeen := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestSeen <- request.Clone(request.Context())
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, validMetadataEnvelope)
	}))
	t.Cleanup(server.Close)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	client, err := media.NewGFSClient(server.URL, httpClient)
	require.NoError(t, err)

	got, err := client.Metadata(context.Background(), 41)

	require.NoError(t, err)
	require.Equal(t, media.Metadata{
		FileID:      41,
		FileName:    "photo.png",
		ContentType: "image/png",
		FileSize:    8192,
		Width:       640,
		Height:      480,
	}, got)
	request := <-requestSeen
	require.Equal(t, http.MethodGet, request.Method)
	require.Equal(t, "/alioss/objects/41/metadata", request.URL.Path)
	require.Empty(t, request.URL.RawQuery)
	require.Empty(t, request.Header.Get("Authorization"))
	require.Equal(t, 5*time.Second, httpClient.Timeout)
}

func TestGFSClientMetadataRejectsUnavailableOrMalformedResponsesWithoutLeaks(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		transport  http.RoundTripper
		context    context.Context
		nilContext bool
		fileID     int64
		secretText string
	}{
		{name: "non-200 status", status: http.StatusBadGateway, body: `upstream-body-secret`, fileID: 41, secretText: "upstream-body-secret"},
		{name: "malformed JSON", status: http.StatusOK, body: `{"secret":"malformed-body-secret"`, fileID: 41, secretText: "malformed-body-secret"},
		{name: "oversized JSON", status: http.StatusOK, body: strings.Repeat("oversized-body-secret", 4097), fileID: 41, secretText: "oversized-body-secret"},
		{name: "nonzero GFS code", status: http.StatusOK, body: `{"code":31001,"msg":"gfs-message-secret","data":null}`, fileID: 41, secretText: "gfs-message-secret"},
		{name: "missing GFS code", status: http.StatusOK, body: strings.Replace(validMetadataEnvelope, `"code":0,`, "", 1), fileID: 41},
		{name: "mismatched ID", status: http.StatusOK, body: strings.Replace(validMetadataEnvelope, `"fileId":41`, `"fileId":42`, 1), fileID: 41},
		{name: "missing image width", status: http.StatusOK, body: strings.Replace(validMetadataEnvelope, `"imageWidth":"640",`, "", 1), fileID: 41},
		{name: "nondecimal image width", status: http.StatusOK, body: strings.Replace(validMetadataEnvelope, `"640"`, `"width-secret"`, 1), fileID: 41, secretText: "width-secret"},
		{name: "zero image height", status: http.StatusOK, body: strings.Replace(validMetadataEnvelope, `"480"`, `"0"`, 1), fileID: 41},
		{name: "trailing JSON", status: http.StatusOK, body: validMetadataEnvelope + `{"secret":"trailing-body-secret"}`, fileID: 41, secretText: "trailing-body-secret"},
		{name: "transport failure", transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("transport-secret https://request-url-secret.example")
		}), fileID: 41, secretText: "transport-secret"},
		{name: "timeout", transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		}), fileID: 41},
		{name: "canceled context", transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return nil, request.Context().Err()
		}), context: canceledContext(), fileID: 41},
		{name: "nil context", nilContext: true, fileID: 41},
		{name: "nonpositive file ID", context: context.Background(), fileID: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseURL := "https://request-url-secret.example"
			client := &http.Client{Timeout: 5 * time.Second, Transport: test.transport}
			requestContext := test.context
			if requestContext == nil && !test.nilContext {
				requestContext = context.Background()
			}
			if test.transport == nil && !test.nilContext && test.fileID > 0 {
				server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
					response.WriteHeader(test.status)
					_, _ = io.WriteString(response, test.body)
				}))
				t.Cleanup(server.Close)
				baseURL = server.URL
			}
			gfs, err := media.NewGFSClient(baseURL, client)
			require.NoError(t, err)

			got, callErr := gfs.Metadata(requestContext, test.fileID)

			require.Equal(t, media.Metadata{}, got)
			require.ErrorIs(t, callErr, media.ErrDependencyUnavailable)
			for _, secret := range []string{test.secretText, "request-url-secret"} {
				if secret != "" {
					require.NotContains(t, callErr.Error(), secret)
				}
			}
		})
	}
}

func TestGFSClientRequiresCanonicalBaseAndFiveSecondClient(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		client  *http.Client
	}{
		{name: "missing base URL", client: &http.Client{Timeout: 5 * time.Second}},
		{name: "malformed base URL", baseURL: "://base-url-secret", client: &http.Client{Timeout: 5 * time.Second}},
		{name: "base URL userinfo", baseURL: "https://url-secret@gfs.example.com", client: &http.Client{Timeout: 5 * time.Second}},
		{name: "base URL query", baseURL: "https://gfs.example.com?secret=query", client: &http.Client{Timeout: 5 * time.Second}},
		{name: "base URL fragment", baseURL: "https://gfs.example.com#secret-fragment", client: &http.Client{Timeout: 5 * time.Second}},
		{name: "base URL path", baseURL: "https://gfs.example.com/secret-path", client: &http.Client{Timeout: 5 * time.Second}},
		{name: "noncanonical root slash", baseURL: "https://gfs.example.com/", client: &http.Client{Timeout: 5 * time.Second}},
		{name: "nil HTTP client", baseURL: "https://gfs.example.com"},
		{name: "zero HTTP timeout", baseURL: "https://gfs.example.com", client: &http.Client{}},
		{name: "wrong HTTP timeout", baseURL: "https://gfs.example.com", client: &http.Client{Timeout: 4 * time.Second}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := media.NewGFSClient(test.baseURL, test.client)

			require.Nil(t, client)
			require.ErrorIs(t, err, media.ErrInvalidGFSConfiguration)
			for _, secret := range []string{"base-url-secret", "url-secret", "query", "secret-fragment", "secret-path"} {
				require.NotContains(t, err.Error(), secret)
			}
		})
	}
}

func TestGFSClientMetadataNilAndZeroReceiversFailSafely(t *testing.T) {
	var nilClient *media.GFSClient
	require.NotPanics(t, func() {
		_, err := nilClient.Metadata(context.Background(), 41)
		require.ErrorIs(t, err, media.ErrDependencyUnavailable)
	})

	zeroClient := &media.GFSClient{}
	require.NotPanics(t, func() {
		_, err := zeroClient.Metadata(context.Background(), 41)
		require.ErrorIs(t, err, media.ErrDependencyUnavailable)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
