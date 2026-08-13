package settings

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/idna"
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

type HotlinkEntry struct {
	ID       int64
	Hostname string
	Enabled  bool
}

type HotlinkPolicy struct {
	AllowEmptyReferer bool
	Entries           []HotlinkEntry
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
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" || parsed.ForceQuery {
		return false
	}
	authority, ok := canonicalSocialAuthority(parsed)
	if !ok {
		return false
	}
	canonicalPath, ok := canonicalURLComponent(parsed.EscapedPath(), pathByteAllowed)
	if !ok || !cleanAbsoluteURLPath(canonicalPath) {
		return false
	}
	canonical := "https://" + authority + canonicalPath
	if parsed.RawQuery != "" {
		query, valid := canonicalURLComponent(parsed.RawQuery, queryOrFragmentByteAllowed)
		if !valid || query == "" {
			return false
		}
		canonical += "?" + query
	}
	fragmentDelimiter := strings.IndexByte(raw, '#') >= 0
	if fragmentDelimiter {
		fragment, valid := canonicalURLComponent(parsed.EscapedFragment(), queryOrFragmentByteAllowed)
		if !valid || fragment == "" {
			return false
		}
		canonical += "#" + fragment
	}
	return raw == canonical
}

func canonicalSocialAuthority(parsed *url.URL) (string, bool) {
	hostname := parsed.Hostname()
	if hostname == "" || strings.Contains(hostname, "%") {
		return "", false
	}
	var host string
	if address, err := netip.ParseAddr(hostname); err == nil {
		if address.Zone() != "" {
			return "", false
		}
		if address.Is4() {
			host = address.String()
		} else {
			host = "[" + address.String() + "]"
		}
	} else {
		if looksLikeNoncanonicalIP(hostname) || strings.HasSuffix(hostname, ".") {
			return "", false
		}
		asciiHost, conversionErr := idna.Lookup.ToASCII(hostname)
		if conversionErr != nil || asciiHost == "" || asciiHost != hostname || asciiHost != strings.ToLower(asciiHost) || !validDNSNameLength(asciiHost) {
			return "", false
		}
		host = asciiHost
	}

	port := parsed.Port()
	if port != "" {
		portNumber, err := strconv.ParseUint(port, 10, 16)
		if err != nil || portNumber == 0 || portNumber == 443 || strconv.FormatUint(portNumber, 10) != port {
			return "", false
		}
		host += ":" + port
	}
	if parsed.Host != host {
		return "", false
	}
	return host, true
}

func looksLikeNoncanonicalIP(host string) bool {
	if strings.Contains(host, ":") {
		return true
	}
	for _, character := range host {
		if character != '.' && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validDNSNameLength(host string) bool {
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
	}
	return true
}

func canonicalURLComponent(raw string, allowed func(byte) bool) (string, bool) {
	var canonical strings.Builder
	canonical.Grow(len(raw))
	for index := 0; index < len(raw); {
		current := raw[index]
		if current == '%' {
			if index+2 >= len(raw) {
				return "", false
			}
			high, highOK := hexadecimalValue(raw[index+1])
			low, lowOK := hexadecimalValue(raw[index+2])
			if !highOK || !lowOK {
				return "", false
			}
			decoded := high<<4 | low
			if unreservedURLByte(decoded) {
				canonical.WriteByte(decoded)
			} else {
				canonical.WriteByte('%')
				canonical.WriteByte(upperHexadecimal(decoded >> 4))
				canonical.WriteByte(upperHexadecimal(decoded & 0x0f))
			}
			index += 3
			continue
		}
		if current < utf8.RuneSelf && allowed(current) {
			canonical.WriteByte(current)
		} else {
			canonical.WriteByte('%')
			canonical.WriteByte(upperHexadecimal(current >> 4))
			canonical.WriteByte(upperHexadecimal(current & 0x0f))
		}
		index++
	}
	return canonical.String(), true
}

func cleanAbsoluteURLPath(value string) bool {
	if value == "" {
		return true
	}
	if value[0] != '/' {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func pathByteAllowed(value byte) bool {
	return unreservedURLByte(value) || strings.ContainsRune("!$&'()*+,;=:@/", rune(value))
}

func queryOrFragmentByteAllowed(value byte) bool {
	return pathByteAllowed(value) || value == '?'
}

func unreservedURLByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || strings.ContainsRune("-._~", rune(value))
}

func hexadecimalValue(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

func upperHexadecimal(value byte) byte {
	if value < 10 {
		return '0' + value
	}
	return 'A' + value - 10
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

func cloneHotlinkPolicy(policy HotlinkPolicy) HotlinkPolicy {
	cloned := policy
	cloned.Entries = make([]HotlinkEntry, len(policy.Entries))
	copy(cloned.Entries, policy.Entries)
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
