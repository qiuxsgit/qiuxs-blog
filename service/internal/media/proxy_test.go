package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const validProxyPublicKey = "m_aaaaaaaaaaaaaaaaaaaaaa"

func TestProxyRedirectAuthorizesRefererBeforeKeyValidationAndLookup(t *testing.T) {
	finder := &activeMediaFinderFake{}
	signer := &readURLSignerFake{}

	for _, referer := range []string{
		"", "https://disabled.example/file", "https://sub.qiuxs.com/file", "https://unlisted.example/file",
		"not-a-url-secret", "ftp://qiuxs.com/file",
	} {
		t.Run(referer, func(t *testing.T) {
			authorizer := &hotlinkAuthorizerFake{allowed: false}
			service := newProxyService(t, authorizer, finder, signer, time.Now)
			_, err := service.Redirect(context.Background(), "malformed-key-secret", referer)

			require.ErrorIs(t, err, ErrHotlinkForbidden)
			require.EqualError(t, err, "authorize media redirect failed")
			if referer != "" {
				require.NotContains(t, err.Error(), referer)
			}
			require.Equal(t, []string{referer}, authorizer.refererCalls)
			require.Empty(t, finder.calls, "forbidden callers must not learn whether a key exists")
			require.Empty(t, signer.calls)
		})
	}
}

func TestProxyRedirectAllowsEmptyFlagAndEnabledExactHostThenSignsStoredMediaLocally(t *testing.T) {
	at := time.Date(2026, 8, 14, 18, 30, 0, 123000, time.FixedZone("CST", 8*60*60))
	item := Media{ID: 31, PublicKey: validProxyPublicKey, GFSFileID: 91, State: "active"}
	target := "https://gfs.example.com/read/signed-policy?signature=signed-secret"

	for _, test := range []struct {
		name    string
		referer string
	}{
		{name: "empty referer allowed", referer: ""},
		{name: "enabled exact hostname", referer: "https://qiuxs.com:8443/path?q=1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			authorizer := &hotlinkAuthorizerFake{allowed: true}
			finder := &activeMediaFinderFake{result: item}
			signer := &readURLSignerFake{result: target}
			service := newProxyService(t, authorizer, finder, signer, func() time.Time { return at })

			got, err := service.Redirect(context.Background(), validProxyPublicKey, test.referer)

			require.NoError(t, err)
			require.Equal(t, target, got)
			require.Equal(t, []string{test.referer}, authorizer.refererCalls)
			require.Equal(t, []string{validProxyPublicKey}, finder.calls)
			require.Equal(t, []readURLCall{{item: item, at: at.UTC()}}, signer.calls)
		})
	}
}

func TestProxyRedirectValidatesKeyAfterAuthorizationWithoutLookup(t *testing.T) {
	authorizer := &hotlinkAuthorizerFake{allowed: true}
	finder := &activeMediaFinderFake{}
	signer := &readURLSignerFake{}
	service := newProxyService(t, authorizer, finder, signer, time.Now)

	for _, key := range []string{"", "91", "M_aaaaaaaaaaaaaaaaaaaaaa", "m_short-secret"} {
		_, err := service.Redirect(context.Background(), key, "")

		require.ErrorIs(t, err, ErrNotFound)
		require.EqualError(t, err, "resolve media redirect failed")
		if key != "" {
			require.NotContains(t, err.Error(), key)
		}
	}
	require.Empty(t, finder.calls)
	require.Empty(t, signer.calls)
}

func TestProxyRedirectMapsPolicyLookupAndSigningFailuresWithoutSecrets(t *testing.T) {
	secret := "proxy-dependency-secret"
	item := Media{ID: 31, PublicKey: validProxyPublicKey, GFSFileID: 91, State: "active"}
	for _, test := range []struct {
		name       string
		authorizer HotlinkAuthorizer
		finder     *activeMediaFinderFake
		signer     *readURLSignerFake
		want       error
	}{
		{name: "policy", authorizer: &hotlinkAuthorizerFake{err: errors.New(secret)}, finder: &activeMediaFinderFake{}, signer: &readURLSignerFake{}, want: ErrDependencyUnavailable},
		{name: "missing active media", authorizer: &hotlinkAuthorizerFake{allowed: true}, finder: &activeMediaFinderFake{err: ErrNotFound}, signer: &readURLSignerFake{}, want: ErrNotFound},
		{name: "media lookup", authorizer: &hotlinkAuthorizerFake{allowed: true}, finder: &activeMediaFinderFake{err: errors.New(secret)}, signer: &readURLSignerFake{}, want: ErrDependencyUnavailable},
		{name: "signing", authorizer: &hotlinkAuthorizerFake{allowed: true}, finder: &activeMediaFinderFake{result: item}, signer: &readURLSignerFake{err: errors.New(secret)}, want: ErrDependencyUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := newProxyService(t, test.authorizer, test.finder, test.signer, time.Now)

			got, err := service.Redirect(context.Background(), validProxyPublicKey, "")

			require.Empty(t, got)
			require.ErrorIs(t, err, test.want)
			require.NotContains(t, err.Error(), secret)
			require.NotContains(t, err.Error(), validProxyPublicKey)
		})
	}
}

func TestProxyServiceRejectsNilDependenciesAndMethodsFailSafely(t *testing.T) {
	authorizer := &hotlinkAuthorizerFake{allowed: true}
	finder := &activeMediaFinderFake{}
	signer := &readURLSignerFake{}
	var typedNilAuthorizer *hotlinkAuthorizerFake
	var typedNilFinder *activeMediaFinderFake
	var typedNilSigner *readURLSignerFake

	for _, test := range []struct {
		name       string
		authorizer HotlinkAuthorizer
		finder     activeMediaFinder
		signer     ReadURLSigner
		now        func() time.Time
	}{
		{name: "nil authorizer", finder: finder, signer: signer, now: time.Now},
		{name: "typed nil authorizer", authorizer: typedNilAuthorizer, finder: finder, signer: signer, now: time.Now},
		{name: "nil finder", authorizer: authorizer, signer: signer, now: time.Now},
		{name: "typed nil finder", authorizer: authorizer, finder: typedNilFinder, signer: signer, now: time.Now},
		{name: "nil signer", authorizer: authorizer, finder: finder, now: time.Now},
		{name: "typed nil signer", authorizer: authorizer, finder: finder, signer: typedNilSigner, now: time.Now},
		{name: "nil clock", authorizer: authorizer, finder: finder, signer: signer},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewProxyService(test.authorizer, test.finder, test.signer, test.now)
			require.Nil(t, service)
			require.Error(t, err)
		})
	}

	var nilService *proxyService
	require.NotPanics(t, func() {
		_, err := nilService.Redirect(context.Background(), validProxyPublicKey, "")
		require.ErrorIs(t, err, ErrDependencyUnavailable)
	})
	valid := newProxyService(t, authorizer, finder, signer, time.Now)
	require.NotPanics(t, func() {
		_, err := valid.Redirect(nil, validProxyPublicKey, "")
		require.ErrorIs(t, err, ErrDependencyUnavailable)
	})
}

type activeMediaFinder interface {
	FindActiveByPublicKey(context.Context, string) (Media, error)
}

type activeMediaFinderFake struct {
	result Media
	err    error
	calls  []string
}

func (f *activeMediaFinderFake) FindActiveByPublicKey(_ context.Context, publicKey string) (Media, error) {
	f.calls = append(f.calls, publicKey)
	return f.result, f.err
}

type readURLCall struct {
	item Media
	at   time.Time
}

type readURLSignerFake struct {
	result string
	err    error
	calls  []readURLCall
}

func (f *readURLSignerFake) ReadURL(item Media, at time.Time) (string, error) {
	f.calls = append(f.calls, readURLCall{item: item, at: at})
	return f.result, f.err
}

type hotlinkAuthorizerFake struct {
	allowed      bool
	err          error
	refererCalls []string
}

func (f *hotlinkAuthorizerFake) AllowsCurrentReferer(_ context.Context, referer string) (bool, error) {
	f.refererCalls = append(f.refererCalls, referer)
	return f.allowed, f.err
}

func newProxyService(t *testing.T, authorizer HotlinkAuthorizer, finder activeMediaFinder, signer ReadURLSigner, now func() time.Time) ProxyService {
	t.Helper()
	service, err := NewProxyService(authorizer, finder, signer, now)
	require.NoError(t, err)
	return service
}
