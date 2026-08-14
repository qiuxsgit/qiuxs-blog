package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/platform"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/release"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestCallbackVerifierClaimsNonceAndBindsExactPayload(t *testing.T) {
	server, client := callbackRedis(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	key := []byte(strings.Repeat("h", 32))
	verifier, err := NewCallbackVerifier(key, client, func() time.Time { return now })
	require.NoError(t, err)
	key[0] = 'x'
	payload := validTestCallbackPayload(now, "nonce_1234567890")
	raw := marshalCallback(t, payload)
	signature := signCallback(keyWithByte('h'), payload.Timestamp, payload.Nonce, raw)

	first, duplicate, err := verifier.VerifyAndClaim(context.Background(), raw, signature)
	require.NoError(t, err)
	require.False(t, duplicate)
	require.Equal(t, payload, first)

	second, duplicate, err := verifier.VerifyAndClaim(context.Background(), raw, signature)
	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, payload, second)

	nonceDigest := sha256.Sum256([]byte(payload.Nonce))
	redisKey := "qiuxs-blog:jenkins:nonce:" + hex.EncodeToString(nonceDigest[:])
	require.Equal(t, []string{redisKey}, server.Keys())
	payloadDigest := sha256.Sum256(raw)
	storedDigest, err := server.Get(redisKey)
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(payloadDigest[:]), storedDigest)
	require.Equal(t, 5*time.Minute, server.TTL(redisKey))
	require.NotContains(t, redisKey, payload.Nonce)

	altered := payload
	altered.PublishJobID = 13
	alteredRaw := marshalCallback(t, altered)
	_, duplicate, err = verifier.VerifyAndClaim(context.Background(), alteredRaw, signCallback(keyWithByte('h'), altered.Timestamp, altered.Nonce, alteredRaw))
	require.ErrorIs(t, err, ErrCallbackReplay)
	require.False(t, duplicate)
	require.NotContains(t, err.Error(), payload.Nonce)
	require.NotContains(t, err.Error(), signature)
}

func TestCallbackVerifierAcceptsInclusiveTimestampWindowWhitespaceAndConcurrency(t *testing.T) {
	_, client := callbackRedis(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	key := keyWithByte('h')
	verifier, err := NewCallbackVerifier(key, client, func() time.Time { return now })
	require.NoError(t, err)

	for index, timestamp := range []time.Time{now.Add(-5 * time.Minute), now.Add(5 * time.Minute)} {
		payload := validTestCallbackPayload(timestamp, "boundary_nonce_"+strconv.Itoa(index))
		raw := append([]byte(" \n\t"), marshalCallback(t, payload)...)
		raw = append(raw, '\n')
		got, duplicate, err := verifier.VerifyAndClaim(context.Background(), raw, signCallback(key, timestamp, payload.Nonce, raw))
		require.NoError(t, err)
		require.False(t, duplicate)
		require.Equal(t, payload, got)
	}

	payload := validTestCallbackPayload(now, "concurrent_nonce")
	raw := marshalCallback(t, payload)
	signature := signCallback(key, payload.Timestamp, payload.Nonce, raw)
	const callers = 16
	var wait sync.WaitGroup
	wait.Add(callers)
	results := make(chan bool, callers)
	errorsSeen := make(chan error, callers)
	for range callers {
		go func() {
			defer wait.Done()
			_, duplicate, verifyErr := verifier.VerifyAndClaim(context.Background(), raw, signature)
			results <- duplicate
			errorsSeen <- verifyErr
		}()
	}
	wait.Wait()
	close(results)
	close(errorsSeen)
	firstClaims := 0
	for verifyErr := range errorsSeen {
		require.NoError(t, verifyErr)
	}
	for duplicate := range results {
		if !duplicate {
			firstClaims++
		}
	}
	require.Equal(t, 1, firstClaims)
}

func TestCallbackVerifierRejectsMalformedOrInvalidPayloadBeforeRedis(t *testing.T) {
	server, client := callbackRedis(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	key := keyWithByte('h')
	verifier, err := NewCallbackVerifier(key, client, func() time.Time { return now })
	require.NoError(t, err)
	valid := string(marshalCallback(t, validTestCallbackPayload(now, "nonce_1234567890")))
	timestamp := now.Format(time.RFC3339Nano)

	tests := map[string][]byte{
		"empty":             nil,
		"invalid utf8":      append([]byte(`{"releaseId":7,"publishJobId":12,"buildNumber":44,"stage":"bu`), 0xff),
		"not object":        []byte(`[]`),
		"unknown":           []byte(strings.TrimSuffix(valid, "}") + `,"secret":"value"}`),
		"duplicate":         []byte(strings.Replace(valid, `"releaseId":7`, `"releaseId":7,"releaseId":7`, 1)),
		"missing":           []byte(strings.Replace(valid, `"errorSummary":"",`, "", 1)),
		"trailing":          []byte(valid + `{}`),
		"quoted id":         []byte(strings.Replace(valid, `"releaseId":7`, `"releaseId":"7"`, 1)),
		"exponent id":       []byte(strings.Replace(valid, `"buildNumber":44`, `"buildNumber":4.4e1`, 1)),
		"invalid nonce":     []byte(strings.Replace(valid, `nonce_1234567890`, `short`, 1)),
		"bad stage pair":    []byte(strings.Replace(valid, `"stage":"queue"`, `"stage":"deploy"`, 1)),
		"summary on queued": []byte(strings.Replace(valid, `"errorSummary":""`, `"errorSummary":"not-failed"`, 1)),
		"lone surrogate":    []byte(strings.Replace(valid, `"errorSummary":""`, `"errorSummary":"\ud800"`, 1)),
		"old timestamp":     []byte(strings.Replace(valid, timestamp, now.Add(-5*time.Minute-time.Nanosecond).Format(time.RFC3339Nano), 1)),
		"future timestamp":  []byte(strings.Replace(valid, timestamp, now.Add(5*time.Minute+time.Nanosecond).Format(time.RFC3339Nano), 1)),
		"oversized":         append([]byte(valid), []byte(strings.Repeat(" ", maxCallbackBodyBytes-len(valid)+1))...),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			_, duplicate, verifyErr := verifier.VerifyAndClaim(context.Background(), raw, "sha256="+strings.Repeat("a", 64))
			require.ErrorIs(t, verifyErr, ErrInvalidCallback)
			require.False(t, duplicate)
			require.Empty(t, server.Keys())
			require.NotContains(t, verifyErr.Error(), "secret")
		})
	}
}

func TestCallbackVerifierRequiresExactSignatureOverExactRawBody(t *testing.T) {
	server, client := callbackRedis(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	key := keyWithByte('h')
	verifier, err := NewCallbackVerifier(key, client, func() time.Time { return now })
	require.NoError(t, err)
	payload := validTestCallbackPayload(now, "nonce_1234567890")
	raw := marshalCallback(t, payload)
	validSignature := signCallback(key, payload.Timestamp, payload.Nonce, raw)

	for name, signature := range map[string]string{
		"missing": "", "no prefix": strings.TrimPrefix(validSignature, "sha256="),
		"uppercase digest": "sha256=" + strings.ToUpper(strings.TrimPrefix(validSignature, "sha256=")),
		"multiple":         validSignature + "," + validSignature, "spaces": " " + validSignature,
		"wrong": "sha256=" + strings.Repeat("a", 64),
	} {
		t.Run(name, func(t *testing.T) {
			_, duplicate, verifyErr := verifier.VerifyAndClaim(context.Background(), raw, signature)
			require.ErrorIs(t, verifyErr, ErrCallbackUnauthorized)
			require.False(t, duplicate)
			require.Empty(t, server.Keys())
			if signature != "" {
				require.NotContains(t, verifyErr.Error(), signature)
			}
		})
	}

	alteredRaw := append(append([]byte(nil), raw...), '\n')
	_, _, err = verifier.VerifyAndClaim(context.Background(), alteredRaw, validSignature)
	require.ErrorIs(t, err, ErrCallbackUnauthorized)
	require.Empty(t, server.Keys())
}

func TestCallbackVerifierDependencyAndNilSafety(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	key := keyWithByte('h')
	server, client := callbackRedis(t)
	verifier, err := NewCallbackVerifier(key, client, func() time.Time { return now })
	require.NoError(t, err)
	payload := validTestCallbackPayload(now, "nonce_1234567890")
	raw := marshalCallback(t, payload)
	signature := signCallback(key, payload.Timestamp, payload.Nonce, raw)
	server.Close()
	require.NoError(t, client.Close())
	_, _, err = verifier.VerifyAndClaim(context.Background(), raw, signature)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), signature)
	require.NotContains(t, err.Error(), payload.Nonce)

	for name, construct := range map[string]func() (*CallbackVerifier, error){
		"short key":  func() (*CallbackVerifier, error) { return NewCallbackVerifier([]byte("secret"), client, time.Now) },
		"nil redis":  func() (*CallbackVerifier, error) { return NewCallbackVerifier(key, nil, time.Now) },
		"zero redis": func() (*CallbackVerifier, error) { return NewCallbackVerifier(key, new(redis.Client), time.Now) },
		"nil clock":  func() (*CallbackVerifier, error) { return NewCallbackVerifier(key, client, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			var got *CallbackVerifier
			require.NotPanics(t, func() { got, err = construct() })
			require.Nil(t, got)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
		})
	}
	var nilVerifier *CallbackVerifier
	require.NotPanics(t, func() { _, _, err = nilVerifier.VerifyAndClaim(context.Background(), raw, signature) })
	require.Error(t, err)
	_, _, err = verifier.VerifyAndClaim(nil, raw, signature)
	require.Error(t, err)

	zeroNowVerifier, err := NewCallbackVerifier(key, callbackClient(t), func() time.Time { return time.Time{} })
	require.NoError(t, err)
	_, _, err = zeroNowVerifier.VerifyAndClaim(context.Background(), raw, signature)
	require.ErrorIs(t, err, ErrDependencyUnavailable)
}

func TestJenkinsTargetProviderLoadsEnabledConfigBeforeReturningExactTrigger(t *testing.T) {
	var networkCalls atomic.Int32
	server := newTLSServer(t, func(releaseID, publishJobID string) {
		networkCalls.Add(1)
		require.Equal(t, "7", releaseID)
		require.Equal(t, "12", publishJobID)
	})
	client, err := NewClient(server.Client())
	require.NoError(t, err)
	config, box := storedClientConfig(t, server.URL, "site/build", true)
	configs := &targetConfigRepository{stored: config}
	provider, err := NewJenkinsTargetProvider(configs, client, box)
	require.NoError(t, err)

	target, err := provider.Prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(7), target.BuilderID)
	require.NotNil(t, target.Trigger)
	require.Equal(t, 1, configs.loadCalls)
	require.Zero(t, networkCalls.Load(), "prepare must not contact Jenkins")
	queueID, err := target.Trigger(context.Background(), 7, 12)
	require.NoError(t, err)
	require.Zero(t, queueID)
	require.Equal(t, int32(1), networkCalls.Load())

	configs.stored.Enabled = false
	_, err = provider.Prepare(context.Background())
	require.ErrorIs(t, err, ErrDisabled)
	require.Equal(t, int32(1), networkCalls.Load())
}

func TestJenkinsTargetProviderFailsSafelyForDependencies(t *testing.T) {
	config, box := storedClientConfig(t, "https://jenkins.example.com", "site/build", true)
	configs := &targetConfigRepository{stored: config, loadErr: errors.New("config-secret")}
	client, err := NewClient(&http.Client{})
	require.NoError(t, err)
	provider, err := NewJenkinsTargetProvider(configs, client, box)
	require.NoError(t, err)
	_, err = provider.Prepare(context.Background())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "config-secret")

	var typedNilConfigs *targetConfigRepository
	for _, construct := range []func() (*JenkinsTargetProvider, error){
		func() (*JenkinsTargetProvider, error) { return NewJenkinsTargetProvider(typedNilConfigs, client, box) },
		func() (*JenkinsTargetProvider, error) { return NewJenkinsTargetProvider(configs, nil, box) },
		func() (*JenkinsTargetProvider, error) { return NewJenkinsTargetProvider(configs, client, nil) },
	} {
		var got *JenkinsTargetProvider
		require.NotPanics(t, func() { got, err = construct() })
		require.Nil(t, got)
		require.Error(t, err)
	}
	var nilProvider *JenkinsTargetProvider
	require.NotPanics(t, func() { _, err = nilProvider.Prepare(context.Background()) })
	require.Error(t, err)
	_, err = provider.Prepare(nil)
	require.Error(t, err)
}

func callbackRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func callbackClient(t *testing.T) *redis.Client {
	t.Helper()
	_, client := callbackRedis(t)
	return client
}

func validTestCallbackPayload(at time.Time, nonce string) CallbackPayload {
	return CallbackPayload{
		ReleaseID: 7, PublishJobID: 12, BuildNumber: 44, Stage: "queue", Status: release.JobQueued,
		ErrorSummary: "", Timestamp: at, Nonce: nonce,
	}
}

func marshalCallback(t *testing.T, payload CallbackPayload) []byte {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return raw
}

func signCallback(key []byte, timestamp time.Time, nonce string, raw []byte) string {
	canonical := strconv.FormatInt(timestamp.Unix(), 10) + "\n" + nonce + "\n" + string(raw)
	return "sha256=" + platform.ComputeHMAC(key, []byte(canonical))
}

func keyWithByte(value byte) []byte { return []byte(strings.Repeat(string(value), 32)) }

type targetConfigRepository struct {
	stored    StoredConfig
	loadErr   error
	loadCalls int
}

func (*targetConfigRepository) Save(context.Context, ConfigInput) (ConfigView, error) {
	return ConfigView{}, errors.New("not configured")
}

func (r *targetConfigRepository) Load(context.Context) (StoredConfig, error) {
	r.loadCalls++
	return r.stored, r.loadErr
}

func newTLSServer(t *testing.T, observe func(string, string)) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.NoError(t, request.ParseForm())
		observe(request.PostForm.Get("RELEASE_ID"), request.PostForm.Get("PUBLISH_JOB_ID"))
		writer.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)
	return server
}
