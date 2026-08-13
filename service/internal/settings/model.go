package settings

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	FilingURL        = "https://beian.miit.gov.cn/"
	maximumAboutSize = 2 * 1024 * 1024
	maximumSocials   = 16
)

type SocialLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type Site struct {
	ID                     int64
	LockVersion            int64
	SiteName               string
	AuthorName             string
	AuthorBio              string
	HomeStatus             string
	AboutMD                string
	SocialLinks            []SocialLink
	SEODefaultTitle        string
	SEODefaultDescription  string
	SEODefaultImageMediaID *int64
	FilingName             string
	FilingNumber           string
	UpdatedAt              time.Time
}

var (
	ErrNotFound = errors.New("settings not found")
	ErrConflict = errors.New("settings optimistic lock conflict")
	ErrInvalid  = errors.New("settings are invalid")
)

func DefaultSite() Site {
	return Site{
		SiteName:     "qiuxs",
		AuthorName:   "qiuxs",
		SocialLinks:  make([]SocialLink, 0),
		FilingName:   "长安休息室",
		FilingNumber: "浙ICP备17057726号-1",
	}
}

func ValidatePublishable(site Site) error {
	if strings.TrimSpace(site.FilingName) == "" || strings.TrimSpace(site.FilingNumber) == "" {
		return settingsDomainError("validate publishable settings", ErrInvalid, nil)
	}
	return nil
}

func normalizeSite(site Site) (Site, error) {
	normalized := cloneSite(site)
	normalized.SiteName = strings.TrimSpace(site.SiteName)
	normalized.AuthorName = strings.TrimSpace(site.AuthorName)
	normalized.AuthorBio = strings.TrimSpace(site.AuthorBio)
	normalized.HomeStatus = strings.TrimSpace(site.HomeStatus)
	normalized.SEODefaultTitle = strings.TrimSpace(site.SEODefaultTitle)
	normalized.SEODefaultDescription = strings.TrimSpace(site.SEODefaultDescription)
	normalized.FilingName = strings.TrimSpace(site.FilingName)
	normalized.FilingNumber = strings.TrimSpace(site.FilingNumber)
	for index := range normalized.SocialLinks {
		normalized.SocialLinks[index].Label = strings.TrimSpace(normalized.SocialLinks[index].Label)
		normalized.SocialLinks[index].URL = strings.TrimSpace(normalized.SocialLinks[index].URL)
	}
	if err := validateNormalizedSite(normalized); err != nil {
		return Site{}, err
	}
	return normalized, nil
}

func validateNormalizedSite(site Site) error {
	return validateSite(site, true)
}

func validateStoredSite(site Site) error {
	return validateSite(site, false)
}

func validateSite(site Site, requireCanonicalFiling bool) error {
	invalidFiling := utf8.RuneCountInString(site.FilingName) > 100 || utf8.RuneCountInString(site.FilingNumber) > 100
	if requireCanonicalFiling {
		invalidFiling = !validRequiredRunes(site.FilingName, 100) || !validRequiredRunes(site.FilingNumber, 100)
	}
	if !validRequiredRunes(site.SiteName, 100) || !validRequiredRunes(site.AuthorName, 100) ||
		site.AuthorBio != strings.TrimSpace(site.AuthorBio) || site.HomeStatus != strings.TrimSpace(site.HomeStatus) ||
		site.SEODefaultTitle != strings.TrimSpace(site.SEODefaultTitle) ||
		site.SEODefaultDescription != strings.TrimSpace(site.SEODefaultDescription) ||
		utf8.RuneCountInString(site.AuthorBio) > 1000 || utf8.RuneCountInString(site.HomeStatus) > 500 ||
		len(site.AboutMD) > maximumAboutSize || utf8.RuneCountInString(site.SEODefaultTitle) > 100 ||
		utf8.RuneCountInString(site.SEODefaultDescription) > 300 || invalidFiling ||
		len(site.SocialLinks) > maximumSocials {
		return settingsDomainError("validate settings", ErrInvalid, nil)
	}
	if site.SEODefaultImageMediaID != nil && *site.SEODefaultImageMediaID <= 0 {
		return settingsDomainError("validate settings", ErrInvalid, nil)
	}
	labels := make(map[string]struct{}, len(site.SocialLinks))
	for _, social := range site.SocialLinks {
		if social.Label == "" || social.Label != strings.TrimSpace(social.Label) || !canonicalSocialURL(social.URL) {
			return settingsDomainError("validate settings", ErrInvalid, nil)
		}
		labelKey := strings.ToLower(social.Label)
		if _, exists := labels[labelKey]; exists {
			return settingsDomainError("validate settings", ErrInvalid, nil)
		}
		labels[labelKey] = struct{}{}
	}
	return nil
}

func canonicalSocialURL(raw string) bool {
	if raw == "" || raw != strings.TrimSpace(raw) || !strings.HasPrefix(raw, "https://") {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" {
		return false
	}
	if parsed.Hostname() == "" || parsed.Hostname() != strings.ToLower(parsed.Hostname()) || parsed.Port() == "443" {
		return false
	}
	return parsed.String() == raw
}

func validRequiredRunes(value string, maximum int) bool {
	return value != "" && value == strings.TrimSpace(value) && utf8.RuneCountInString(value) <= maximum
}

func cloneSite(site Site) Site {
	cloned := site
	cloned.SocialLinks = make([]SocialLink, len(site.SocialLinks))
	copy(cloned.SocialLinks, site.SocialLinks)
	if site.SEODefaultImageMediaID != nil {
		id := *site.SEODefaultImageMediaID
		cloned.SEODefaultImageMediaID = &id
	}
	return cloned
}

type safeError struct {
	operation string
	cause     error
}

func (e *safeError) Error() string { return e.operation + " failed" }
func (e *safeError) Unwrap() error { return e.cause }

func settingsDependencyError(operation string, cause error) error {
	return &safeError{operation: operation, cause: cause}
}

type domainError struct {
	operation string
	domain    error
	cause     error
}

func (e *domainError) Error() string { return e.operation + " failed" }
func (e *domainError) Unwrap() []error {
	if e.cause == nil {
		return []error{e.domain}
	}
	return []error{e.domain, e.cause}
}

func settingsDomainError(operation string, domain, cause error) error {
	if domain == nil {
		return fmt.Errorf("%s failed", operation)
	}
	return &domainError{operation: operation, domain: domain, cause: cause}
}
