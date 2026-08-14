package builder

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
	"github.com/stretchr/testify/require"
)

func TestJenkinsClientTestUsesExactTLSOriginAndDoesNotMutateInjectedClient(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/json", request.URL.EscapedPath())
		require.Empty(t, request.URL.RawQuery)
		username, token, ok := request.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "ci", username)
		require.Equal(t, "private-token", token)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"mode":"NORMAL"}`))
	}))
	defer server.Close()
	injected := server.Client()
	injected.Timeout = 0
	require.Nil(t, injected.CheckRedirect)
	client, err := NewClient(injected)
	require.NoError(t, err)
	cfg, box := storedClientConfig(t, server.URL, "site/build", true)

	err = client.Test(context.Background(), cfg, box)
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load())
	require.Nil(t, injected.CheckRedirect)
	require.Zero(t, injected.Timeout)
}

func TestJenkinsClientDoesNotUseOrMutateInjectedCookieJar(t *testing.T) {
	var observedCookie string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observedCookie = request.Header.Get("Cookie")
		http.SetCookie(writer, &http.Cookie{Name: "jenkins_session", Value: "server-secret", Path: "/"})
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	origin, err := url.Parse(server.URL)
	require.NoError(t, err)
	jar.SetCookies(origin, []*http.Cookie{{Name: "caller_session", Value: "private-cookie", Path: "/"}})
	injected := server.Client()
	injected.Jar = jar
	client, err := NewClient(injected)
	require.NoError(t, err)
	cfg, box := storedClientConfig(t, server.URL, "site/build", true)

	require.NoError(t, client.Test(context.Background(), cfg, box))
	require.Empty(t, observedCookie)
	require.Equal(t, []*http.Cookie{{Name: "caller_session", Value: "private-cookie"}}, jar.Cookies(origin))
}

func TestNewJenkinsClientRejectsTypedNilTransportWithoutMutationOrPanic(t *testing.T) {
	var typedNil *panicTransport
	injected := &http.Client{Transport: typedNil, Timeout: 3 * time.Second}
	var client *Client
	var err error
	require.NotPanics(t, func() { client, err = NewClient(injected) })
	require.Nil(t, client)
	require.EqualError(t, err, "Jenkins HTTP client transport is not configured")
	require.Equal(t, 3*time.Second, injected.Timeout)
	require.Same(t, typedNil, injected.Transport)

	defaultTransportClient, err := NewClient(&http.Client{})
	require.NoError(t, err)
	require.NotNil(t, defaultTransportClient)
	require.Nil(t, defaultTransportClient.httpClient.Transport)
	require.Same(t, typedNil, injected.Transport)
}

func TestJenkinsClientPublicMethodsRejectTypedNilTransportWithoutPanic(t *testing.T) {
	var typedNil *panicTransport
	injected := &http.Client{Transport: typedNil, Timeout: 3 * time.Second}
	unsafeClient := &Client{httpClient: injected}
	cfg, box := storedClientConfig(t, "https://jenkins.example.com", "site/build", true)
	var err error
	require.NotPanics(t, func() { err = unsafeClient.Test(context.Background(), cfg, box) })
	require.EqualError(t, err, "Jenkins HTTP client transport is not configured")
	require.NotPanics(t, func() { _, err = unsafeClient.Trigger(context.Background(), cfg, box, 9, 12) })
	require.EqualError(t, err, "Jenkins HTTP client transport is not configured")
	require.Same(t, typedNil, injected.Transport)
}

func TestJenkinsClientTriggerEscapesSegmentsAndSendsOnlyCorrelatedIDs(t *testing.T) {
	var observedPath string
	var observedForm url.Values
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observedPath = request.URL.EscapedPath()
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		username, token, ok := request.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "ci", username)
		require.Equal(t, "private-token", token)
		require.NoError(t, request.ParseForm())
		observedForm = request.PostForm
		writer.Header().Set("X-Jenkins-Queue-Id", "55")
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client, err := NewClient(server.Client())
	require.NoError(t, err)
	cfg, box := storedClientConfig(t, server.URL, "site/build-name_1", true)

	queueID, err := client.Trigger(context.Background(), cfg, box, 9, 12)
	require.NoError(t, err)
	require.Equal(t, int64(55), queueID)
	require.Equal(t, "/job/site/job/build-name_1/buildWithParameters", observedPath)
	require.Equal(t, url.Values{"RELEASE_ID": {"9"}, "PUBLISH_JOB_ID": {"12"}}, observedForm)
}

func TestJenkinsClientAcceptsDirect302WithoutFollowingOrForwardingAuth(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer target.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _, ok := request.BasicAuth()
		require.True(t, ok)
		writer.Header().Set("Location", target.URL+"/credential-leak")
		writer.WriteHeader(http.StatusFound)
	}))
	defer origin.Close()
	client, err := NewClient(origin.Client())
	require.NoError(t, err)
	cfg, box := storedClientConfig(t, origin.URL, "site/build", true)

	queueID, err := client.Trigger(context.Background(), cfg, box, 9, 12)
	require.NoError(t, err)
	require.Zero(t, queueID)
	require.Zero(t, redirected.Load())
}

func TestJenkinsClientRejectsStatusQueueAndBodyFailuresSanitizedAndClosed(t *testing.T) {
	for name, queue := range map[string][]string{
		"zero": {"0"}, "negative": {"-1"}, "plus": {"+1"}, "leading zero": {"01"},
		"decimal": {"1.0"}, "exponent": {"1e1"}, "overflow": {"9223372036854775808"}, "multiple": {"1", "2"},
	} {
		t.Run("queue "+name, func(t *testing.T) {
			body := &trackingBody{Reader: strings.NewReader("queue-secret")}
			client, err := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				header := make(http.Header)
				for _, value := range queue {
					header.Add("X-Jenkins-Queue-Id", value)
				}
				return &http.Response{StatusCode: http.StatusCreated, Header: header, Body: body}, nil
			})})
			require.NoError(t, err)
			cfg, box := storedClientConfig(t, "https://jenkins.example.com", "site/build", true)
			_, err = client.Trigger(context.Background(), cfg, box, 9, 12)
			require.ErrorIs(t, err, ErrDependencyUnavailable)
			require.NotContains(t, err.Error(), "queue-secret")
			require.True(t, body.closed)
		})
	}

	for name, response := range map[string]*http.Response{
		"test unauthorized": {StatusCode: http.StatusUnauthorized, Body: &trackingBody{Reader: strings.NewReader("auth-secret")}},
		"trigger server":    {StatusCode: http.StatusInternalServerError, Body: &trackingBody{Reader: strings.NewReader("server-secret")}},
		"oversized":         {StatusCode: http.StatusOK, Body: &trackingBody{Reader: strings.NewReader(strings.Repeat("x", maxJenkinsResponseBytes+1))}},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response, nil })})
			require.NoError(t, err)
			cfg, box := storedClientConfig(t, "https://jenkins.example.com", "site/build", true)
			if strings.HasPrefix(name, "trigger") {
				_, err = client.Trigger(context.Background(), cfg, box, 9, 12)
			} else {
				err = client.Test(context.Background(), cfg, box)
			}
			require.ErrorIs(t, err, ErrDependencyUnavailable)
			require.NotContains(t, err.Error(), "secret")
			require.True(t, response.Body.(*trackingBody).closed)
		})
	}
}

func TestJenkinsClientRespectsEarlierContextAndRejectsInvalidDependenciesBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	client, err := NewClient(&http.Client{Transport: transport})
	require.NoError(t, err)
	cfg, box := storedClientConfig(t, "https://jenkins.example.com", "site/build", true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = client.Test(ctx, cfg, box)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), time.Second)
	require.Equal(t, int32(1), calls.Load())

	disabled := cfg
	disabled.Enabled = false
	err = client.Test(context.Background(), disabled, box)
	require.ErrorIs(t, err, ErrDisabled)
	corrupt := cfg
	corrupt.EncryptedToken = "ciphertext-secret"
	err = client.Test(context.Background(), corrupt, box)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	_, err = client.Trigger(context.Background(), cfg, box, 0, 12)
	require.ErrorIs(t, err, ErrInvalidConfig)
	_, err = client.Trigger(context.Background(), cfg, box, 9, 0)
	require.ErrorIs(t, err, ErrInvalidConfig)
	err = client.Test(nil, cfg, box)
	require.Error(t, err)
	var nilBox *platform.SecretBox
	err = client.Test(context.Background(), cfg, nilBox)
	require.Error(t, err)
	zeroBox := &platform.SecretBox{}
	err = client.Test(context.Background(), cfg, zeroBox)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	var nilClient *Client
	err = nilClient.Test(context.Background(), cfg, box)
	require.Error(t, err)
	zeroClient := &Client{}
	err = zeroClient.Test(context.Background(), cfg, box)
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load(), "invalid dependencies must not reach transport")
}

func TestJenkinsClientRejectsAuthenticatedCiphertextCorruptionAndWrongKeyBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	client, err := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}, nil
	})})
	require.NoError(t, err)
	cfg, box := storedClientConfig(t, "https://jenkins.example.com", "site/build", true)
	decoded, err := base64.RawStdEncoding.Strict().DecodeString(cfg.EncryptedToken)
	require.NoError(t, err)
	decoded[len(decoded)-1] ^= 1
	tampered := cfg
	tampered.EncryptedToken = base64.RawStdEncoding.EncodeToString(decoded)
	err = client.Test(context.Background(), tampered, box)
	require.ErrorIs(t, err, ErrDependencyUnavailable)

	wrongBox, err := platform.NewSecretBox(bytes.Repeat([]byte{9}, 32), bytes.NewReader(bytes.Repeat([]byte{4}, 12)))
	require.NoError(t, err)
	err = client.Test(context.Background(), cfg, &wrongBox)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.Zero(t, calls.Load())
}

func TestJenkinsClientUsesTenSecondDerivedDeadlineAndRejectsTypedNilBody(t *testing.T) {
	var remaining time.Duration
	injected := &http.Client{Timeout: time.Nanosecond, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		require.True(t, ok)
		remaining = time.Until(deadline)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}, nil
	})}
	client, err := NewClient(injected)
	require.NoError(t, err)
	cfg, box := storedClientConfig(t, "https://jenkins.example.com", "site/build", true)
	require.NoError(t, client.Test(context.Background(), cfg, box))
	require.Greater(t, remaining, 9*time.Second)
	require.LessOrEqual(t, remaining, jenkinsRequestTimeout)
	require.Equal(t, time.Nanosecond, injected.Timeout)

	var typedNilBody *trackingBody
	require.NotPanics(t, func() {
		_, _, err = consumeJenkinsResponse(&http.Response{StatusCode: http.StatusOK, Body: typedNilBody})
	})
	require.ErrorIs(t, err, ErrDependencyUnavailable)
}

func TestJenkinsClientNetworkAndCloseErrorsAreSanitized(t *testing.T) {
	client, err := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network-secret https://credential.example.com")
	})})
	require.NoError(t, err)
	cfg, box := storedClientConfig(t, "https://jenkins.example.com", "site/build", true)
	err = client.Test(context.Background(), cfg, box)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), "network-secret")
	require.NotContains(t, err.Error(), "credential.example.com")

	body := &trackingBody{Reader: bytes.NewReader(nil), closeErr: errors.New("close-secret")}
	client, err = NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
	})})
	require.NoError(t, err)
	err = client.Test(context.Background(), cfg, box)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), "close-secret")
}

func storedClientConfig(t *testing.T, baseURL, jobName string, enabled bool) (StoredConfig, *platform.SecretBox) {
	t.Helper()
	box, err := platform.NewSecretBox(bytes.Repeat([]byte{8}, 32), bytes.NewReader(bytes.Repeat([]byte{3}, 12*8)))
	require.NoError(t, err)
	ciphertext, err := box.Seal([]byte("private-token"))
	require.NoError(t, err)
	return StoredConfig{ConfigView: ConfigView{
		ID: 7, Name: "Production", BaseURL: baseURL, Username: "ci", JobName: jobName, Enabled: enabled, TokenConfigured: true,
	}, EncryptedToken: ciphertext}, &box
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type panicTransport struct{}

func (*panicTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("typed-nil transport must not be used")
}

type trackingBody struct {
	io.Reader
	closed   bool
	closeErr error
}

func (body *trackingBody) Close() error {
	body.closed = true
	return body.closeErr
}
