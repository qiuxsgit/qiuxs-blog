package media_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
	"github.com/stretchr/testify/require"
)

const (
	testRawAppSecret     = "raw-app-secret"
	testPublicReadSecret = "public-read-secret"
	testUploadPolicy     = "eyJzYXZlUGF0aCI6ImJsb2cve3t5ZWFyfX0ve3ttb250aH19L3t7dXVpZH19Lnt7ZmlsZUV4dH19In0="
	testReadPolicy       = "eyJ1c2VySWQiOiIiLCJmaWxlSWQiOjkxLCJpbWFnZVdpZHRoIjowLCJpbWFnZUhlaWdodCI6MCwiaW50ZXJuYWxGbGFnIjowfQ=="
)

func TestGFSSignerUploadPolicyMatchesFixedGFSVector(t *testing.T) {
	keys, err := randomkey.New(bytes.NewReader(append(byteRange(26, 38), byteRange(38, 48)...)))
	require.NoError(t, err)
	signer, err := media.NewGFSSigner("https://gfs.example.com", "blog-app", testRawAppSecret, testPublicReadSecret, keys)
	require.NoError(t, err)

	got, err := signer.UploadPolicy(time.Unix(1700000000, 999).UTC())

	require.NoError(t, err)
	require.Equal(t, media.UploadPolicy{
		UploadURL: "https://gfs.example.com/v1/upload",
		AppID:     "blog-app",
		Policy:    testUploadPolicy,
		Signature: "9306fd28436522a60b1e4798e786b300",
		Timestamp: "1700000000",
		Expire:    "60",
		Nonce:     "0123456789-_abcdefghij",
		FileField: "file",
	}, got)

	decoded, err := base64.StdEncoding.DecodeString(got.Policy)
	require.NoError(t, err)
	var policy map[string]string
	require.NoError(t, json.Unmarshal(decoded, &policy))
	require.Equal(t, map[string]string{"savePath": "blog/{{year}}/{{month}}/{{uuid}}.{{fileExt}}"}, policy)
}

func TestGFSSignerReadURLMatchesFixedGFSVectorAndEscapesPolicy(t *testing.T) {
	keys, err := randomkey.New(bytes.NewReader(make([]byte, 22)))
	require.NoError(t, err)
	signer, err := media.NewGFSSigner("https://gfs.example.com", "blog-app", testRawAppSecret, testPublicReadSecret, keys)
	require.NoError(t, err)

	got, err := signer.ReadURL(media.Media{GFSFileID: 91}, time.Unix(1700000000, 999).UTC())

	require.NoError(t, err)
	require.Equal(t, "https://gfs.example.com/read/"+testReadPolicy+"?expire=60&signature=bbaae35530a6f4d9f01187ecbe901f97&timestamp=1700000000", got)
	parsed, err := url.Parse(got)
	require.NoError(t, err)
	require.Equal(t, "/read/"+testReadPolicy, parsed.Path)
	require.Equal(t, "60", parsed.Query().Get("expire"))
	require.Equal(t, "bbaae35530a6f4d9f01187ecbe901f97", parsed.Query().Get("signature"))
	require.Equal(t, "1700000000", parsed.Query().Get("timestamp"))

	decoded, err := base64.StdEncoding.DecodeString(testReadPolicy)
	require.NoError(t, err)
	var policy map[string]any
	require.NoError(t, json.Unmarshal(decoded, &policy))
	require.Equal(t, map[string]any{
		"userId":       "",
		"fileId":       float64(91),
		"imageWidth":   float64(0),
		"imageHeight":  float64(0),
		"internalFlag": float64(0),
	}, policy)
}

func TestGFSSignerRejectsInvalidConfigurationWithoutRevealingValues(t *testing.T) {
	validKeys, err := randomkey.New(bytes.NewReader(make([]byte, 64)))
	require.NoError(t, err)
	tests := []struct {
		name       string
		baseURL    string
		appID      string
		appSecret  string
		readSecret string
		keys       *randomkey.Generator
	}{
		{name: "missing base URL", appID: "app-secret-value", appSecret: testRawAppSecret, readSecret: testPublicReadSecret, keys: validKeys},
		{name: "malformed base URL", baseURL: "://base-url-secret", appID: "app-secret-value", appSecret: testRawAppSecret, readSecret: testPublicReadSecret, keys: validKeys},
		{name: "base URL userinfo", baseURL: "https://url-secret@gfs.example.com", appID: "app-secret-value", appSecret: testRawAppSecret, readSecret: testPublicReadSecret, keys: validKeys},
		{name: "base URL query", baseURL: "https://gfs.example.com?secret=query", appID: "app-secret-value", appSecret: testRawAppSecret, readSecret: testPublicReadSecret, keys: validKeys},
		{name: "base URL path", baseURL: "https://gfs.example.com/secret-path", appID: "app-secret-value", appSecret: testRawAppSecret, readSecret: testPublicReadSecret, keys: validKeys},
		{name: "noncanonical root slash", baseURL: "https://gfs.example.com/", appID: "app-secret-value", appSecret: testRawAppSecret, readSecret: testPublicReadSecret, keys: validKeys},
		{name: "missing app ID", baseURL: "https://gfs.example.com", appSecret: testRawAppSecret, readSecret: testPublicReadSecret, keys: validKeys},
		{name: "missing app secret", baseURL: "https://gfs.example.com", appID: "app-secret-value", readSecret: testPublicReadSecret, keys: validKeys},
		{name: "missing public read secret", baseURL: "https://gfs.example.com", appID: "app-secret-value", appSecret: testRawAppSecret, keys: validKeys},
		{name: "missing key generator", baseURL: "https://gfs.example.com", appID: "app-secret-value", appSecret: testRawAppSecret, readSecret: testPublicReadSecret},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signer, callErr := media.NewGFSSigner(test.baseURL, test.appID, test.appSecret, test.readSecret, test.keys)
			require.Error(t, callErr)
			require.Nil(t, signer)
			for _, secret := range []string{"base-url-secret", "url-secret", "query", "secret-path", "app-secret-value", testRawAppSecret, testPublicReadSecret} {
				require.NotContains(t, callErr.Error(), secret)
			}
		})
	}
}

func TestGFSSignerMethodsFailSafelyWithoutLeakingSecretsOrSignatures(t *testing.T) {
	var nilSigner *media.GFSSigner
	require.NotPanics(t, func() {
		_, err := nilSigner.UploadPolicy(time.Unix(1700000000, 0))
		require.Error(t, err)
	})
	require.NotPanics(t, func() {
		_, err := nilSigner.ReadURL(media.Media{GFSFileID: 91}, time.Unix(1700000000, 0))
		require.Error(t, err)
	})
	zeroSigner := &media.GFSSigner{}
	require.NotPanics(t, func() {
		_, err := zeroSigner.UploadPolicy(time.Unix(1700000000, 0))
		require.Error(t, err)
	})
	require.NotPanics(t, func() {
		_, err := zeroSigner.ReadURL(media.Media{GFSFileID: 91}, time.Unix(1700000000, 0))
		require.Error(t, err)
	})

	failingKeys, err := randomkey.New(errorReader{})
	require.NoError(t, err)
	signer, err := media.NewGFSSigner("https://gfs.example.com", "blog-app", testRawAppSecret, testPublicReadSecret, failingKeys)
	require.NoError(t, err)
	_, err = signer.UploadPolicy(time.Unix(1700000000, 0))
	require.Error(t, err)
	for _, secret := range []string{testRawAppSecret, testPublicReadSecret, "random-source-secret", "9306fd28436522a60b1e4798e786b300"} {
		require.NotContains(t, err.Error(), secret)
	}

	_, err = signer.ReadURL(media.Media{}, time.Unix(1700000000, 0))
	require.Error(t, err)
	for _, secret := range []string{testRawAppSecret, testPublicReadSecret, "bbaae35530a6f4d9f01187ecbe901f97"} {
		require.NotContains(t, err.Error(), secret)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, &secretError{}
}

type secretError struct{}

func (*secretError) Error() string { return "random-source-secret" }

func byteRange(start, end byte) []byte {
	values := make([]byte, 0, int(end-start))
	for value := start; value < end; value++ {
		values = append(values, value)
	}
	return values
}
