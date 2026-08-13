package settings

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeHostnameReturnsCanonicalASCIIDNSName(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: " qiuxs.COM. ", want: "qiuxs.com"},
		{input: "blog-admin.qiuxs.com", want: "blog-admin.qiuxs.com"},
		{input: "localhost", want: "localhost"},
		{input: strings.Repeat("a", 63) + ".example", want: strings.Repeat("a", 63) + ".example"},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, err := NormalizeHostname(test.input)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestNormalizeHostnameRejectsNonDNSAndAmbiguousInputsWithoutLeak(t *testing.T) {
	tooLong := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 62)
	for _, input := range []string{
		"", " ", ".", "example.com..", "example..com", "-example.com", "example-.com",
		"*.example.com", "foo.*.com", "https://example.com", "example.com:443", "example.com/path",
		"example.com?query", "example.com#fragment", "127.0.0.1", "[2001:db8::1]", "2001:db8::1",
		"bücher.example", "under_score.example", strings.Repeat("a", 64) + ".example", tooLong,
	} {
		t.Run(input, func(t *testing.T) {
			got, err := NormalizeHostname(input)
			require.Empty(t, got)
			require.ErrorIs(t, err, ErrInvalid)
			require.EqualError(t, err, "normalize hotlink hostname failed")
		})
	}
}

func TestHotlinkGetReturnsExactVirtualDefaultOnlyForMissingSingleton(t *testing.T) {
	repository := &hotlinkRepositoryFake{getErr: ErrNotFound}
	service := newHotlinkService(t, repository, time.Now)

	first, err := service.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, HotlinkPolicy{
		AllowEmptyReferer: true,
		Entries: []HotlinkEntry{
			{Hostname: "qiuxs.com", Enabled: true},
			{Hostname: "blog-admin.qiuxs.com", Enabled: true},
		},
	}, first)
	require.NotNil(t, first.Entries)
	first.Entries[0].Hostname = "mutated"

	second, err := service.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, "qiuxs.com", second.Entries[0].Hostname)

	repository.setGet(HotlinkPolicy{AllowEmptyReferer: false, Entries: []HotlinkEntry{}}, nil)
	persistedEmpty, err := service.Get(context.Background())
	require.NoError(t, err)
	require.False(t, persistedEmpty.AllowEmptyReferer)
	require.NotNil(t, persistedEmpty.Entries)
	require.Empty(t, persistedEmpty.Entries)
}

func TestHotlinkGetSanitizesDependencyFailure(t *testing.T) {
	repository := &hotlinkRepositoryFake{getErr: errors.New("hotlink-database-secret")}
	service := newHotlinkService(t, repository, time.Now)

	_, err := service.Get(context.Background())

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotFound)
	require.NotContains(t, err.Error(), "hotlink-database-secret")
}

func TestHotlinkPutNormalizesEntriesRejectsDuplicatesAndDoesNotAlias(t *testing.T) {
	at := time.Date(2026, 8, 14, 20, 0, 0, 123000, time.FixedZone("CST", 8*60*60))
	repository := &hotlinkRepositoryFake{replaceResult: HotlinkPolicy{
		AllowEmptyReferer: false,
		Entries:           []HotlinkEntry{{ID: 11, Hostname: "example.com", Enabled: true}},
	}}
	service := newHotlinkService(t, repository, func() time.Time { return at })
	input := []HotlinkEntry{{ID: 999, Hostname: " EXAMPLE.COM. ", Enabled: true}}

	got, err := service.Put(context.Background(), false, input)
	require.NoError(t, err)
	require.Len(t, repository.replaceCalls, 1)
	require.Equal(t, hotlinkReplaceCall{
		policy: HotlinkPolicy{AllowEmptyReferer: false, Entries: []HotlinkEntry{{Hostname: "example.com", Enabled: true}}},
		at:     at.UTC(),
	}, repository.replaceCalls[0])

	input[0].Hostname = "mutated-input"
	got.Entries[0].Hostname = "mutated-output"
	require.Equal(t, "example.com", repository.replaceCalls[0].policy.Entries[0].Hostname)
	require.Equal(t, "example.com", repository.replaceResult.Entries[0].Hostname)

	for _, entries := range [][]HotlinkEntry{
		{{Hostname: "example.com"}, {Hostname: "EXAMPLE.COM."}},
		{{Hostname: "invalid:443"}},
	} {
		before := len(repository.replaceCalls)
		_, err := service.Put(context.Background(), true, entries)
		require.ErrorIs(t, err, ErrInvalid)
		require.Len(t, repository.replaceCalls, before)
	}
}

func TestHotlinkAllowsOnlyEmptyFlagOrEnabledExactHTTPRefererHost(t *testing.T) {
	service := newHotlinkService(t, &hotlinkRepositoryFake{}, time.Now)
	policy := HotlinkPolicy{
		AllowEmptyReferer: true,
		Entries: []HotlinkEntry{
			{Hostname: "qiuxs.com", Enabled: true},
			{Hostname: "disabled.example", Enabled: false},
		},
	}

	for _, referer := range []string{
		"", "https://qiuxs.com", "http://qiuxs.com:8080/path?q=value", "https://QIUXS.COM./preview", "HTTPS://qiuxs.com/path",
	} {
		require.True(t, service.AllowsReferer(policy, referer), referer)
	}
	policy.AllowEmptyReferer = false
	for _, referer := range []string{
		"", " ", "qiuxs.com", "/relative", "ftp://qiuxs.com/file", "https://sub.qiuxs.com/file",
		"https://disabled.example/file", "https://unlisted.example/file", "https://127.0.0.1/file",
		"https://user@qiuxs.com/file", "https:///path", "https://qiuxs.com:bad/file", "://malformed-secret",
	} {
		require.False(t, service.AllowsReferer(policy, referer), referer)
	}
}

func TestHotlinkAllowsCurrentRefererUsesExactDecisionAndCurrentCache(t *testing.T) {
	repository := &hotlinkRepositoryFake{getResult: HotlinkPolicy{
		AllowEmptyReferer: false,
		Entries: []HotlinkEntry{
			{Hostname: "qiuxs.com", Enabled: true},
			{Hostname: "disabled.example", Enabled: false},
		},
	}}
	service := newHotlinkService(t, repository, time.Now)

	for _, test := range []struct {
		referer string
		want    bool
	}{
		{referer: "", want: false},
		{referer: "https://qiuxs.com/image.png", want: true},
		{referer: "https://sub.qiuxs.com/image.png", want: false},
		{referer: "https://disabled.example/image.png", want: false},
		{referer: "ftp://qiuxs.com/image.png", want: false},
		{referer: "malformed-secret", want: false},
	} {
		allowed, err := service.AllowsCurrentReferer(context.Background(), test.referer)
		require.NoError(t, err)
		require.Equal(t, test.want, allowed, test.referer)
	}
	require.Equal(t, 1, repository.getCallCount(), "current policy must be cached across decisions")
}

func TestHotlinkAllowsCurrentRefererSanitizesDependencyFailureAndDoesNotCacheIt(t *testing.T) {
	repository := &hotlinkRepositoryFake{getErr: errors.New("policy-secret")}
	service := newHotlinkService(t, repository, time.Now)

	for range 2 {
		allowed, err := service.AllowsCurrentReferer(context.Background(), "https://referer-secret.example/image.png")
		require.False(t, allowed)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "policy-secret")
		require.NotContains(t, err.Error(), "referer-secret")
	}
	require.Equal(t, 2, repository.getCallCount(), "dependency errors must not be cached")
}

func TestHotlinkAllowsCurrentRefererReloadsAfterSuccessfulPut(t *testing.T) {
	oldPolicy := HotlinkPolicy{Entries: []HotlinkEntry{{Hostname: "old.example", Enabled: true}}}
	newPolicy := HotlinkPolicy{Entries: []HotlinkEntry{{ID: 2, Hostname: "new.example", Enabled: true}}}
	repository := &hotlinkRepositoryFake{getResult: oldPolicy}
	service := newHotlinkService(t, repository, time.Now)

	allowed, err := service.AllowsCurrentReferer(context.Background(), "https://old.example/image.png")
	require.NoError(t, err)
	require.True(t, allowed)
	repository.setReplace(newPolicy, nil)
	_, err = service.Put(context.Background(), false, []HotlinkEntry{{Hostname: "new.example", Enabled: true}})
	require.NoError(t, err)

	allowed, err = service.AllowsCurrentReferer(context.Background(), "https://old.example/image.png")
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = service.AllowsCurrentReferer(context.Background(), "https://new.example/image.png")
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 2, repository.getCallCount())
}

func TestHotlinkCurrentLoadsOnceReturnsImmutableCopiesAndDoesNotCacheErrors(t *testing.T) {
	policy := HotlinkPolicy{AllowEmptyReferer: true, Entries: []HotlinkEntry{{ID: 2, Hostname: "qiuxs.com", Enabled: true}}}
	repository := &hotlinkRepositoryFake{getResult: policy}
	service := newHotlinkService(t, repository, time.Now)

	first, err := service.Current(context.Background())
	require.NoError(t, err)
	first.Entries[0].Hostname = "mutated"
	second, err := service.Current(context.Background())
	require.NoError(t, err)
	require.Equal(t, "qiuxs.com", second.Entries[0].Hostname)
	require.Equal(t, 1, repository.getCallCount())

	failing := &hotlinkRepositoryFake{getErr: errors.New("cache-load-secret")}
	failingService := newHotlinkService(t, failing, time.Now)
	for range 2 {
		_, err := failingService.Current(context.Background())
		require.Error(t, err)
		require.NotContains(t, err.Error(), "cache-load-secret")
	}
	require.Equal(t, 2, failing.getCallCount())
}

func TestHotlinkPutFailureKeepsCacheAndSuccessSynchronouslyInvalidates(t *testing.T) {
	oldPolicy := HotlinkPolicy{AllowEmptyReferer: true, Entries: []HotlinkEntry{{ID: 2, Hostname: "old.example", Enabled: true}}}
	newPolicy := HotlinkPolicy{AllowEmptyReferer: false, Entries: []HotlinkEntry{{ID: 5, Hostname: "new.example", Enabled: true}}}
	repository := &hotlinkRepositoryFake{getResult: oldPolicy}
	service := newHotlinkService(t, repository, time.Now)

	_, err := service.Current(context.Background())
	require.NoError(t, err)
	repository.setReplace(HotlinkPolicy{}, errors.New("replace-secret"))
	_, err = service.Put(context.Background(), false, []HotlinkEntry{{Hostname: "new.example", Enabled: true}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "replace-secret")
	stillOld, err := service.Current(context.Background())
	require.NoError(t, err)
	require.Equal(t, oldPolicy, stillOld)
	require.Equal(t, 1, repository.getCallCount())

	repository.setReplace(newPolicy, nil)
	stored, err := service.Put(context.Background(), false, []HotlinkEntry{{Hostname: "new.example", Enabled: true}})
	require.NoError(t, err)
	require.Equal(t, newPolicy, stored)
	afterWrite, err := service.Current(context.Background())
	require.NoError(t, err)
	require.Equal(t, newPolicy, afterWrite)
	require.Equal(t, 2, repository.getCallCount())
}

func TestHotlinkCurrentCoalescesConcurrentMissesWithoutHoldingCacheLockOverRepository(t *testing.T) {
	policy := HotlinkPolicy{AllowEmptyReferer: true, Entries: []HotlinkEntry{{Hostname: "qiuxs.com", Enabled: true}}}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	repository := &hotlinkRepositoryFake{
		getResult: policy, getStarted: started, getRelease: release,
		replaceResult: HotlinkPolicy{AllowEmptyReferer: false, Entries: []HotlinkEntry{{Hostname: "new.example", Enabled: true}}},
	}
	service := newHotlinkService(t, repository, time.Now)

	const callers = 8
	results := make(chan HotlinkPolicy, callers)
	errorsChannel := make(chan error, callers)
	for range callers {
		go func() {
			got, err := service.Current(context.Background())
			results <- got
			errorsChannel <- err
		}()
	}
	awaitSignal(t, started)
	require.Eventually(t, func() bool { return repository.getCallCount() == 1 }, time.Second, time.Millisecond)

	putDone := make(chan error, 1)
	go func() {
		_, err := service.Put(context.Background(), false, []HotlinkEntry{{Hostname: "new.example", Enabled: true}})
		putDone <- err
	}()
	select {
	case err := <-putDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Put blocked behind cache mutex while repository read was in flight")
	}
	close(release)

	for range callers {
		require.NoError(t, <-errorsChannel)
		got := <-results
		require.Equal(t, "new.example", got.Entries[0].Hostname)
	}
	require.Equal(t, 2, repository.getCallCount(), "stale in-flight load must retry once after write invalidation")
}

func TestHotlinkCurrentDiscardsStaleLoadErrorAfterSuccessfulPut(t *testing.T) {
	newPolicy := HotlinkPolicy{AllowEmptyReferer: false, Entries: []HotlinkEntry{{Hostname: "new.example", Enabled: true}}}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	repository := &hotlinkRepositoryFake{
		getErr: errors.New("stale-load-secret"), getStarted: started, getRelease: release,
		replaceResult: newPolicy,
	}
	service := newHotlinkService(t, repository, time.Now)

	result := make(chan HotlinkPolicy, 1)
	errorsChannel := make(chan error, 1)
	go func() {
		got, err := service.Current(context.Background())
		result <- got
		errorsChannel <- err
	}()
	awaitSignal(t, started)

	_, err := service.Put(context.Background(), false, []HotlinkEntry{{Hostname: "new.example", Enabled: true}})
	require.NoError(t, err)
	close(release)

	require.NoError(t, <-errorsChannel)
	require.Equal(t, newPolicy, <-result)
	require.Equal(t, 2, repository.getCallCount(), "the invalidated error must be retried rather than returned")
}

func TestHotlinkCurrentAfterPutStartsFreshLoadBeforeStaleLoadReturns(t *testing.T) {
	oldPolicy := HotlinkPolicy{Entries: []HotlinkEntry{{Hostname: "old.example", Enabled: true}}}
	newPolicy := HotlinkPolicy{Entries: []HotlinkEntry{{Hostname: "new.example", Enabled: true}}}
	oldStarted := make(chan struct{}, 1)
	oldRelease := make(chan struct{})
	newStarted := make(chan struct{}, 1)
	repository := &hotlinkRepositoryFake{
		replaceResult: newPolicy,
		getSteps: []hotlinkGetStep{
			{result: oldPolicy, started: oldStarted, release: oldRelease, ignoreContext: true},
			{result: newPolicy, started: newStarted},
		},
	}
	service := newHotlinkService(t, repository, time.Now)

	firstResult := currentHotlinkAsync(service, context.Background())
	awaitSignal(t, oldStarted)
	_, err := service.Put(context.Background(), false, []HotlinkEntry{{Hostname: "new.example", Enabled: true}})
	require.NoError(t, err)

	secondResult := currentHotlinkAsync(service, context.Background())
	select {
	case <-newStarted:
	case <-time.After(100 * time.Millisecond):
		close(oldRelease)
		awaitHotlinkResult(t, firstResult)
		awaitHotlinkResult(t, secondResult)
		t.Fatal("Current started after Put joined the stale pre-write load")
	}
	require.Equal(t, currentHotlinkResult{policy: newPolicy}, awaitHotlinkResult(t, secondResult))

	close(oldRelease)
	require.Equal(t, currentHotlinkResult{policy: newPolicy}, awaitHotlinkResult(t, firstResult))
	cached, err := service.Current(context.Background())
	require.NoError(t, err)
	require.Equal(t, newPolicy, cached)
	require.Equal(t, 2, repository.getCallCount())
}

func TestStaleHotlinkLoadCompletionDoesNotDetachOrOverwriteNewFlight(t *testing.T) {
	oldPolicy := HotlinkPolicy{Entries: []HotlinkEntry{{Hostname: "old.example", Enabled: true}}}
	newPolicy := HotlinkPolicy{Entries: []HotlinkEntry{{Hostname: "new.example", Enabled: true}}}
	oldStarted := make(chan struct{}, 1)
	oldRelease := make(chan struct{})
	newStarted := make(chan struct{}, 1)
	newRelease := make(chan struct{})
	unexpectedStarted := make(chan struct{}, 1)
	repository := &hotlinkRepositoryFake{
		replaceResult: newPolicy,
		getSteps: []hotlinkGetStep{
			{result: oldPolicy, started: oldStarted, release: oldRelease, ignoreContext: true},
			{result: newPolicy, started: newStarted, release: newRelease},
			{result: HotlinkPolicy{Entries: []HotlinkEntry{{Hostname: "unexpected.example"}}}, started: unexpectedStarted},
		},
	}
	service := newHotlinkService(t, repository, time.Now)
	oldContext, cancelOld := context.WithCancel(context.Background())

	oldResult := currentHotlinkAsync(service, oldContext)
	awaitSignal(t, oldStarted)
	_, err := service.Put(context.Background(), false, []HotlinkEntry{{Hostname: "new.example", Enabled: true}})
	require.NoError(t, err)
	newResult := currentHotlinkAsync(service, context.Background())
	awaitSignal(t, newStarted)

	cancelOld()
	close(oldRelease)
	require.Error(t, awaitHotlinkResult(t, oldResult).err)
	require.Equal(t, 2, repository.getCallCount(), "stale completion must leave the newer flight registered")
	select {
	case <-unexpectedStarted:
		t.Fatal("stale completion detached the newer flight and started a third load")
	default:
	}

	joinedResult := currentHotlinkAsync(service, context.Background())
	require.Never(t, func() bool { return repository.getCallCount() != 2 }, 20*time.Millisecond, time.Millisecond)
	close(newRelease)
	require.Equal(t, currentHotlinkResult{policy: newPolicy}, awaitHotlinkResult(t, newResult))
	require.Equal(t, currentHotlinkResult{policy: newPolicy}, awaitHotlinkResult(t, joinedResult))

	cached, err := service.Current(context.Background())
	require.NoError(t, err)
	require.Equal(t, newPolicy, cached)
	require.Equal(t, 2, repository.getCallCount())
}

func TestHotlinkCurrentCoalescesConcurrentLoadFailureWithoutCachingIt(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	repository := &hotlinkRepositoryFake{
		getErr: errors.New("shared-load-secret"), getStarted: started, getRelease: release,
	}
	service := newHotlinkService(t, repository, time.Now)

	const callers = 8
	errorsChannel := make(chan error, callers)
	go func() {
		_, err := service.Current(context.Background())
		errorsChannel <- err
	}()
	awaitSignal(t, started)
	ready := make(chan struct{}, callers-1)
	for range callers - 1 {
		go func() {
			ready <- struct{}{}
			_, err := service.Current(context.Background())
			errorsChannel <- err
		}()
	}
	for range callers - 1 {
		<-ready
	}
	require.Never(t, func() bool { return len(errorsChannel) != 0 }, 20*time.Millisecond, time.Millisecond)
	close(release)

	for range callers {
		err := <-errorsChannel
		require.Error(t, err)
		require.NotContains(t, err.Error(), "shared-load-secret")
	}
	require.Equal(t, 1, repository.getCallCount(), "callers attached to one failed load must share its outcome")

	repository.setGet(HotlinkPolicy{Entries: []HotlinkEntry{}}, nil)
	_, err := service.Current(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, repository.getCallCount(), "a completed failure must not be cached for later calls")
}

func TestHotlinkCurrentRetriesWhenSharedLoadLeaderIsCanceled(t *testing.T) {
	policy := HotlinkPolicy{Entries: []HotlinkEntry{{Hostname: "healthy.example", Enabled: true}}}
	started := make(chan struct{}, 1)
	repository := &hotlinkRepositoryFake{getResult: policy, getStarted: started, getWaitForContext: true}
	service := newHotlinkService(t, repository, time.Now)
	leaderContext, cancelLeader := context.WithCancel(context.Background())
	leaderError := make(chan error, 1)
	go func() {
		_, err := service.Current(leaderContext)
		leaderError <- err
	}()
	awaitSignal(t, started)

	waiterResult := make(chan HotlinkPolicy, 1)
	waiterError := make(chan error, 1)
	waiterReady := make(chan struct{}, 1)
	go func() {
		waiterReady <- struct{}{}
		got, err := service.Current(context.Background())
		waiterResult <- got
		waiterError <- err
	}()
	<-waiterReady
	require.Never(t, func() bool { return len(waiterError) != 0 }, 20*time.Millisecond, time.Millisecond)
	require.Equal(t, 1, repository.getCallCount())
	cancelLeader()

	require.Error(t, <-leaderError)
	require.NoError(t, <-waiterError)
	require.Equal(t, policy, <-waiterResult)
	require.Equal(t, 2, repository.getCallCount(), "healthy waiter must retry a leader-context failure")
}

func TestHotlinkCurrentCoalescesContextSentinelFromDependencyWhenLeaderIsHealthy(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	repository := &hotlinkRepositoryFake{
		getErr: context.DeadlineExceeded, getStarted: started, getRelease: release,
	}
	service := newHotlinkService(t, repository, time.Now)

	const callers = 8
	errorsChannel := make(chan error, callers)
	go func() {
		_, err := service.Current(context.Background())
		errorsChannel <- err
	}()
	awaitSignal(t, started)
	ready := make(chan struct{}, callers-1)
	for range callers - 1 {
		go func() {
			ready <- struct{}{}
			_, err := service.Current(context.Background())
			errorsChannel <- err
		}()
	}
	for range callers - 1 {
		<-ready
	}
	require.Never(t, func() bool { return len(errorsChannel) != 0 }, 20*time.Millisecond, time.Millisecond)
	close(release)

	for range callers {
		require.Error(t, <-errorsChannel)
	}
	require.Equal(t, 1, repository.getCallCount(), "a context sentinel from a healthy leader's dependency is one shared failure")
}

func TestHotlinkServiceRejectsNilDependenciesAndContextsWithoutPanic(t *testing.T) {
	repository := &hotlinkRepositoryFake{}
	var typedNilRepository *hotlinkRepositoryFake
	for _, test := range []struct {
		name       string
		repository HotlinkRepository
		now        func() time.Time
	}{
		{name: "nil repository", now: time.Now},
		{name: "typed nil repository", repository: typedNilRepository, now: time.Now},
		{name: "nil clock", repository: repository},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewHotlinkService(test.repository, test.now)
			require.Nil(t, service)
			require.Error(t, err)
		})
	}

	var nilService *hotlinkService
	require.NotPanics(t, func() { _, err := nilService.Get(context.Background()); require.Error(t, err) })
	require.NotPanics(t, func() { _, err := nilService.Current(context.Background()); require.Error(t, err) })
	valid := newHotlinkService(t, repository, time.Now)
	require.NotPanics(t, func() { _, err := valid.Get(nil); require.ErrorIs(t, err, ErrInvalid) })
	require.NotPanics(t, func() { _, err := valid.Put(nil, true, nil); require.ErrorIs(t, err, ErrInvalid) })
	require.NotPanics(t, func() { _, err := valid.Current(nil); require.ErrorIs(t, err, ErrInvalid) })
}

type hotlinkReplaceCall struct {
	policy HotlinkPolicy
	at     time.Time
}

type hotlinkRepositoryFake struct {
	mu                sync.Mutex
	getResult         HotlinkPolicy
	getErr            error
	replaceResult     HotlinkPolicy
	replaceErr        error
	getCalls          int
	replaceCalls      []hotlinkReplaceCall
	getStarted        chan struct{}
	getRelease        chan struct{}
	getWaitForContext bool
	getSteps          []hotlinkGetStep
}

type hotlinkGetStep struct {
	result        HotlinkPolicy
	err           error
	started       chan struct{}
	release       <-chan struct{}
	ignoreContext bool
}

func (f *hotlinkRepositoryFake) GetHotlinkPolicy(ctx context.Context) (HotlinkPolicy, error) {
	f.mu.Lock()
	callIndex := f.getCalls
	f.getCalls++
	result := cloneHotlinkPolicy(f.getResult)
	err := f.getErr
	started := f.getStarted
	var release <-chan struct{} = f.getRelease
	waitForContext := f.getWaitForContext
	ignoreContext := false
	if callIndex < len(f.getSteps) {
		step := f.getSteps[callIndex]
		result = cloneHotlinkPolicy(step.result)
		err = step.err
		started = step.started
		release = step.release
		ignoreContext = step.ignoreContext
	}
	f.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		if ignoreContext {
			<-release
		} else {
			select {
			case <-release:
			case <-ctx.Done():
				return HotlinkPolicy{}, ctx.Err()
			}
		}
	}
	if waitForContext {
		f.mu.Lock()
		f.getWaitForContext = false
		f.mu.Unlock()
		<-ctx.Done()
		return HotlinkPolicy{}, ctx.Err()
	}
	return result, err
}

type currentHotlinkResult struct {
	policy HotlinkPolicy
	err    error
}

func currentHotlinkAsync(service HotlinkService, ctx context.Context) <-chan currentHotlinkResult {
	result := make(chan currentHotlinkResult, 1)
	go func() {
		policy, err := service.Current(ctx)
		result <- currentHotlinkResult{policy: policy, err: err}
	}()
	return result
}

func awaitHotlinkResult(t *testing.T, result <-chan currentHotlinkResult) currentHotlinkResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hotlink result")
		return currentHotlinkResult{}
	}
}

func (f *hotlinkRepositoryFake) ReplaceHotlinkPolicy(_ context.Context, policy HotlinkPolicy, at time.Time) (HotlinkPolicy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replaceCalls = append(f.replaceCalls, hotlinkReplaceCall{policy: cloneHotlinkPolicy(policy), at: at})
	if f.replaceErr != nil {
		return HotlinkPolicy{}, f.replaceErr
	}
	result := cloneHotlinkPolicy(f.replaceResult)
	if result.Entries == nil {
		result = cloneHotlinkPolicy(policy)
	}
	f.getResult = cloneHotlinkPolicy(result)
	f.getErr = nil
	return result, nil
}

func (f *hotlinkRepositoryFake) setGet(policy HotlinkPolicy, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getResult, f.getErr = cloneHotlinkPolicy(policy), err
}

func (f *hotlinkRepositoryFake) setReplace(policy HotlinkPolicy, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replaceResult, f.replaceErr = cloneHotlinkPolicy(policy), err
}

func (f *hotlinkRepositoryFake) getCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getCalls
}

func newHotlinkService(t *testing.T, repository HotlinkRepository, now func() time.Time) HotlinkService {
	t.Helper()
	service, err := NewHotlinkService(repository, now)
	require.NoError(t, err)
	return service
}

func awaitSignal(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}
