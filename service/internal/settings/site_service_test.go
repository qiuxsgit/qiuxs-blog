package settings

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/media"
	"github.com/stretchr/testify/require"
)

func TestDefaultSiteIsExactVirtualNonAliasingValue(t *testing.T) {
	first := DefaultSite()
	second := DefaultSite()

	require.Equal(t, Site{
		SiteName: "qiuxs", AuthorName: "qiuxs", SocialLinks: []SocialLink{},
		FilingName: "长安休息室", FilingNumber: "浙ICP备17057726号-1",
	}, first)
	require.NotNil(t, first.SocialLinks)
	first.SocialLinks = append(first.SocialLinks, SocialLink{Label: "changed", URL: "https://example.com"})
	require.Empty(t, second.SocialLinks)
	require.Equal(t, "https://beian.miit.gov.cn/", FilingURL)
}

func TestValidatePublishableRequiresFilingWithoutLeakingValue(t *testing.T) {
	require.NoError(t, ValidatePublishable(DefaultSite()))

	for _, test := range []struct {
		name string
		edit func(*Site)
	}{
		{name: "blank filing name", edit: func(site *Site) { site.FilingName = " \t" }},
		{name: "blank filing number", edit: func(site *Site) { site.FilingNumber = "\n" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			site := DefaultSite()
			test.edit(&site)
			err := ValidatePublishable(site)
			require.ErrorIs(t, err, ErrInvalid)
			require.NotContains(t, err.Error(), site.FilingName)
			require.NotContains(t, err.Error(), site.FilingNumber)
		})
	}
}

func TestSiteServiceGetReturnsVirtualDefaultOnlyWhenMissing(t *testing.T) {
	repository := &siteRepositoryFake{getErr: ErrNotFound}
	service := newSiteService(t, repository, &activeMediaFake{}, time.Now)

	got, err := service.GetSite(context.Background())
	require.NoError(t, err)
	require.Equal(t, DefaultSite(), got)

	repository.getErr = errors.New("mysql-get-secret")
	_, err = service.GetSite(context.Background())
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotFound)
	require.NotContains(t, err.Error(), "mysql-get-secret")
}

func TestSiteServicePutNormalizesDisplayFieldsPreservesAboutAndDoesNotAlias(t *testing.T) {
	at := time.Date(2026, 8, 14, 17, 5, 0, 123000, time.FixedZone("CST", 8*60*60))
	resultImageID := int64(31)
	repository := &siteRepositoryFake{createResult: Site{
		ID: 2, LockVersion: 1,
		SocialLinks:            []SocialLink{{Label: "stored", URL: "https://example.com"}},
		SEODefaultImageMediaID: &resultImageID,
	}}
	service := newSiteService(t, repository, &activeMediaFake{}, func() time.Time { return at })
	imageID := int64(31)
	input := Site{
		SiteName: "  qiuxs blog  ", AuthorName: "  Qiuxs  ", AuthorBio: " bio ", HomeStatus: " home ",
		AboutMD: "  # Keep exact  \n", SocialLinks: []SocialLink{{Label: " GitHub ", URL: " https://github.com/qiuxsgit?tab=repositories#readme "}},
		SEODefaultTitle: " SEO ", SEODefaultDescription: " description ", SEODefaultImageMediaID: &imageID,
		FilingName: " 长安休息室 ", FilingNumber: " 浙ICP备17057726号-1 ",
	}

	got, err := service.PutSite(context.Background(), input, 0)
	require.NoError(t, err)
	require.Len(t, repository.createCalls, 1)
	call := repository.createCalls[0]
	require.Equal(t, at.UTC(), call.at)
	require.Equal(t, "qiuxs blog", call.site.SiteName)
	require.Equal(t, "Qiuxs", call.site.AuthorName)
	require.Equal(t, "bio", call.site.AuthorBio)
	require.Equal(t, "home", call.site.HomeStatus)
	require.Equal(t, "  # Keep exact  \n", call.site.AboutMD)
	require.Equal(t, []SocialLink{{Label: "GitHub", URL: "https://github.com/qiuxsgit?tab=repositories#readme"}}, call.site.SocialLinks)
	require.Equal(t, "SEO", call.site.SEODefaultTitle)
	require.Equal(t, "description", call.site.SEODefaultDescription)
	require.Equal(t, "长安休息室", call.site.FilingName)
	require.Equal(t, "浙ICP备17057726号-1", call.site.FilingNumber)

	input.SocialLinks[0].Label = "mutated"
	imageID = 99
	require.Equal(t, "GitHub", call.site.SocialLinks[0].Label)
	require.Equal(t, int64(31), *call.site.SEODefaultImageMediaID)
	got.SocialLinks[0].Label = "changed-output"
	*got.SEODefaultImageMediaID = 77
	require.Equal(t, "stored", repository.createResult.SocialLinks[0].Label)
	require.Equal(t, int64(31), *repository.createResult.SEODefaultImageMediaID)
}

func TestSiteServiceValidatesEveryBoundaryBeforePersistence(t *testing.T) {
	exact2MiB := strings.Repeat("a", 2*1024*1024)
	valid := DefaultSite()
	valid.SiteName = strings.Repeat("界", 100)
	valid.AuthorName = strings.Repeat("界", 100)
	valid.AuthorBio = strings.Repeat("界", 1000)
	valid.HomeStatus = strings.Repeat("界", 500)
	valid.SEODefaultTitle = strings.Repeat("界", 100)
	valid.SEODefaultDescription = strings.Repeat("界", 300)
	valid.FilingName = strings.Repeat("界", 100)
	valid.FilingNumber = strings.Repeat("界", 100)
	valid.AboutMD = exact2MiB
	valid.SocialLinks = makeSocials(16)
	service := newSiteService(t, &siteRepositoryFake{}, &activeMediaFake{}, time.Now)

	_, err := service.PutSite(context.Background(), valid, 0)
	require.NoError(t, err)

	tests := []struct {
		name string
		edit func(*Site)
	}{
		{name: "blank site name", edit: func(s *Site) { s.SiteName = " " }},
		{name: "site name over 100 runes", edit: func(s *Site) { s.SiteName = strings.Repeat("界", 101) }},
		{name: "blank author", edit: func(s *Site) { s.AuthorName = "\t" }},
		{name: "author over 100 runes", edit: func(s *Site) { s.AuthorName = strings.Repeat("界", 101) }},
		{name: "bio over 1000 runes", edit: func(s *Site) { s.AuthorBio = strings.Repeat("界", 1001) }},
		{name: "status over 500 runes", edit: func(s *Site) { s.HomeStatus = strings.Repeat("界", 501) }},
		{name: "about over 2 MiB bytes", edit: func(s *Site) { s.AboutMD = exact2MiB + "x" }},
		{name: "SEO title over 100 runes", edit: func(s *Site) { s.SEODefaultTitle = strings.Repeat("界", 101) }},
		{name: "SEO description over 300 runes", edit: func(s *Site) { s.SEODefaultDescription = strings.Repeat("界", 301) }},
		{name: "filing name over database width", edit: func(s *Site) { s.FilingName = strings.Repeat("界", 101) }},
		{name: "filing number over database width", edit: func(s *Site) { s.FilingNumber = strings.Repeat("界", 101) }},
		{name: "too many socials", edit: func(s *Site) { s.SocialLinks = makeSocials(17) }},
		{name: "blank social label", edit: func(s *Site) { s.SocialLinks = []SocialLink{{Label: " ", URL: "https://example.com"}} }},
		{name: "duplicate social label case insensitive", edit: func(s *Site) {
			s.SocialLinks = []SocialLink{{Label: "GitHub", URL: "https://example.com/a"}, {Label: "github", URL: "https://example.com/b"}}
		}},
		{name: "HTTP social URL", edit: func(s *Site) { s.SocialLinks = []SocialLink{{Label: "site", URL: "http://example.com"}} }},
		{name: "social userinfo", edit: func(s *Site) { s.SocialLinks = []SocialLink{{Label: "site", URL: "https://user@example.com"}} }},
		{name: "relative social URL", edit: func(s *Site) { s.SocialLinks = []SocialLink{{Label: "site", URL: "/profile"}} }},
		{name: "uppercase scheme", edit: func(s *Site) { s.SocialLinks = []SocialLink{{Label: "site", URL: "HTTPS://example.com"}} }},
		{name: "uppercase host", edit: func(s *Site) { s.SocialLinks = []SocialLink{{Label: "site", URL: "https://EXAMPLE.com"}} }},
		{name: "default port", edit: func(s *Site) { s.SocialLinks = []SocialLink{{Label: "site", URL: "https://example.com:443"}} }},
		{name: "zero SEO media", edit: func(s *Site) {
			zero := int64(0)
			s.SEODefaultImageMediaID = &zero
		}},
		{name: "negative expected lock", edit: func(s *Site) {}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := DefaultSite()
			test.edit(&input)
			expectedLock := int64(0)
			if test.name == "negative expected lock" {
				expectedLock = -1
			}
			repository := &siteRepositoryFake{}
			svc := newSiteService(t, repository, &activeMediaFake{}, time.Now)
			_, err := svc.PutSite(context.Background(), input, expectedLock)
			require.ErrorIs(t, err, ErrInvalid)
			require.Empty(t, repository.createCalls)
			require.Empty(t, repository.updateCalls)
		})
	}
}

func TestSiteServiceRequiresExactCanonicalSocialHTTPSURLs(t *testing.T) {
	valid := []string{
		"https://example.com",
		"https://example.com/",
		"https://example.com/profile/sub/?tab=posts&sort=new#latest",
		"https://example.com:8443/a~b?q=x%2Fy#part%20two",
		"https://xn--bcher-kva.example/~user",
		"https://192.0.2.1/profile",
		"https://[2001:db8::1]:8443/a%2Fb",
	}
	for _, socialURL := range valid {
		t.Run("accept "+socialURL, func(t *testing.T) {
			repository := &siteRepositoryFake{}
			service := newSiteService(t, repository, &activeMediaFake{}, time.Now)
			site := DefaultSite()
			site.SocialLinks = []SocialLink{{Label: "Profile", URL: socialURL}}

			_, err := service.PutSite(context.Background(), site, 0)

			require.NoError(t, err)
			require.Len(t, repository.createCalls, 1)
		})
	}

	invalid := []string{
		"https://example.com?",
		"https://example.com?#fragment",
		"https://example.com#",
		"https://example.com:",
		"https://example.com:abc",
		"https://example.com:65536",
		"https://example.com:0444",
		"https://example.com:443",
		"https://example.com:0443",
		"https://EXAMPLE.com/profile",
		"https://bücher.example/profile",
		"https://XN--BCHER-KVA.example/profile",
		"https://example.com./profile",
		"https://192.168.001.001/profile",
		"https://[2001:0db8:0:0:0:0:0:1]/profile",
		"https://example.com/a/./b",
		"https://example.com/a/../b",
		"https://example.com/a/%2e/b",
		"https://example.com/a/%2E%2e/b",
		"https://example.com/a//b",
		"https://example.com/a%2F..%2Fb",
		"https://example.com/a%2F%2Fb",
		"https://example.com/%7euser",
		"https://example.com/%7Euser",
		"https://example.com/a%2fb",
		"https://example.com/中文",
		"https://example.com/%zz",
		"https://example.com/?q=%7euser",
		"https://example.com/#part%2done",
		"https://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.example/profile",
		"https://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/profile",
	}
	for _, socialURL := range invalid {
		t.Run("reject "+socialURL, func(t *testing.T) {
			repository := &siteRepositoryFake{}
			service := newSiteService(t, repository, &activeMediaFake{}, time.Now)
			site := DefaultSite()
			site.SocialLinks = []SocialLink{{Label: "Profile", URL: socialURL}}

			_, err := service.PutSite(context.Background(), site, 0)

			require.ErrorIs(t, err, ErrInvalid)
			require.Empty(t, repository.createCalls)
		})
	}
}

func TestSiteServiceRequiresActiveSEOImageBeforeWrite(t *testing.T) {
	repository := &siteRepositoryFake{}
	mediaValidator := &activeMediaFake{}
	service := newSiteService(t, repository, mediaValidator, time.Now)
	imageID := int64(31)
	input := DefaultSite()
	input.SEODefaultImageMediaID = &imageID

	_, err := service.PutSite(context.Background(), input, 0)
	require.NoError(t, err)
	require.Equal(t, []int64{31}, mediaValidator.calls)

	mediaValidator.err = media.ErrNotFound
	_, err = service.PutSite(context.Background(), input, 0)
	require.ErrorIs(t, err, ErrInvalid)

	mediaValidator.err = errors.New("media-store-secret")
	_, err = service.PutSite(context.Background(), input, 0)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrInvalid)
	require.NotContains(t, err.Error(), "media-store-secret")
}

func TestSiteServiceDispatchesCreateAndOptimisticUpdateAndPreservesDomains(t *testing.T) {
	at := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	repository := &siteRepositoryFake{updateResult: Site{ID: 2, LockVersion: 3}}
	service := newSiteService(t, repository, &activeMediaFake{}, func() time.Time { return at })

	_, err := service.PutSite(context.Background(), DefaultSite(), 2)
	require.NoError(t, err)
	require.Len(t, repository.updateCalls, 1)
	require.Equal(t, int64(2), repository.updateCalls[0].expectedLock)
	require.Equal(t, at, repository.updateCalls[0].at)

	repository.updateErr = ErrConflict
	_, err = service.PutSite(context.Background(), DefaultSite(), 2)
	require.ErrorIs(t, err, ErrConflict)

	repository.updateErr = errors.New("update-secret")
	_, err = service.PutSite(context.Background(), DefaultSite(), 2)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "update-secret")
}

func TestSiteServiceRejectsNilDependenciesAndContextWithoutPanic(t *testing.T) {
	repository := &siteRepositoryFake{}
	validator := &activeMediaFake{}
	var typedNilRepository *siteRepositoryFake
	var typedNilValidator *activeMediaFake

	for _, test := range []struct {
		name       string
		repository SiteRepository
		validator  ActiveMediaValidator
		now        func() time.Time
	}{
		{name: "nil repository", validator: validator, now: time.Now},
		{name: "typed nil repository", repository: typedNilRepository, validator: validator, now: time.Now},
		{name: "nil media validator", repository: repository, now: time.Now},
		{name: "typed nil media validator", repository: repository, validator: typedNilValidator, now: time.Now},
		{name: "nil clock", repository: repository, validator: validator},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewSiteService(test.repository, test.validator, test.now)
			require.Nil(t, got)
			require.Error(t, err)
		})
	}

	var nilService *siteService
	require.NotPanics(t, func() { _, err := nilService.GetSite(context.Background()); require.Error(t, err) })
	valid := newSiteService(t, repository, validator, time.Now)
	require.NotPanics(t, func() { _, err := valid.GetSite(nil); require.ErrorIs(t, err, ErrInvalid) })
	require.NotPanics(t, func() { _, err := valid.PutSite(nil, DefaultSite(), 0); require.ErrorIs(t, err, ErrInvalid) })
}

type siteWriteCall struct {
	site         Site
	expectedLock int64
	at           time.Time
}

type siteRepositoryFake struct {
	getResult    Site
	getErr       error
	createResult Site
	createErr    error
	updateResult Site
	updateErr    error
	createCalls  []siteWriteCall
	updateCalls  []siteWriteCall
}

func (f *siteRepositoryFake) GetSite(context.Context) (Site, error) {
	return f.getResult, f.getErr
}
func (f *siteRepositoryFake) CreateSite(_ context.Context, site Site, at time.Time) (Site, error) {
	f.createCalls = append(f.createCalls, siteWriteCall{site: cloneSite(site), at: at})
	result := f.createResult
	if result.SocialLinks == nil {
		result.SocialLinks = []SocialLink{}
	}
	return result, f.createErr
}
func (f *siteRepositoryFake) UpdateSite(_ context.Context, site Site, expectedLock int64, at time.Time) (Site, error) {
	f.updateCalls = append(f.updateCalls, siteWriteCall{site: cloneSite(site), expectedLock: expectedLock, at: at})
	return f.updateResult, f.updateErr
}

type activeMediaFake struct {
	calls []int64
	err   error
}

func (f *activeMediaFake) RequireActive(_ context.Context, id int64) error {
	f.calls = append(f.calls, id)
	return f.err
}

func newSiteService(t *testing.T, repository SiteRepository, validator ActiveMediaValidator, now func() time.Time) SiteService {
	t.Helper()
	service, err := NewSiteService(repository, validator, now)
	require.NoError(t, err)
	return service
}

func makeSocials(count int) []SocialLink {
	links := make([]SocialLink, count)
	for index := range count {
		links[index] = SocialLink{Label: "social-" + string(rune('a'+index)), URL: "https://example.com/" + string(rune('a'+index))}
	}
	return links
}
