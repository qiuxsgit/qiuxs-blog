package builder

import (
	"errors"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxBuilderNameRunes     = 100
	maxBuilderUsernameRunes = 255
	maxBuilderTokenRunes    = 4096
	maxBuilderJobBytes      = 128
)

var (
	ErrInvalidConfig         = errors.New("builder configuration is invalid")
	ErrNotFound              = errors.New("builder configuration not found")
	ErrConflict              = errors.New("builder configuration conflict")
	ErrDisabled              = errors.New("builder is disabled")
	ErrDependencyUnavailable = errors.New("builder dependency unavailable")

	jobNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
)

type ConfigInput struct {
	Name     string
	BaseURL  string
	Username string
	Token    string
	JobName  string
	Enabled  bool
}

type ConfigView struct {
	ID              int64
	Name            string
	BaseURL         string
	Username        string
	JobName         string
	Enabled         bool
	TokenConfigured bool
}

type StoredConfig struct {
	ConfigView
	EncryptedToken string
}

func ValidateConfig(input ConfigInput) error {
	if !validTrimmed(input.Name, maxBuilderNameRunes) ||
		!validTrimmed(input.Username, maxBuilderUsernameRunes) || strings.Contains(input.Username, ":") ||
		!utf8.ValidString(input.Token) || utf8.RuneCountInString(input.Token) > maxBuilderTokenRunes ||
		!canonicalJenkinsOrigin(input.BaseURL) || !validJobName(input.JobName) {
		return builderDomain("validate builder configuration", ErrInvalidConfig)
	}
	return nil
}

func validTrimmed(value string, maximum int) bool {
	return utf8.ValidString(value) && value != "" && value == strings.TrimSpace(value) && utf8.RuneCountInString(value) <= maximum
}

func validJobName(value string) bool {
	if len(value) == 0 || len(value) > maxBuilderJobBytes || !utf8.ValidString(value) || !jobNamePattern.MatchString(value) {
		return false
	}
	segments := strings.Split(value, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func canonicalJenkinsOrigin(raw string) bool {
	if raw == "" || !utf8.ValidString(raw) || strings.ContainsAny(raw, "?#") {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	hostname := parsed.Hostname()
	if hostname == "" || strings.Contains(hostname, "%") || hostname != strings.ToLower(hostname) || strings.HasSuffix(hostname, ".") {
		return false
	}
	if address, parseErr := netip.ParseAddr(hostname); parseErr == nil {
		if !address.Is4() || address.String() != hostname {
			return false
		}
	} else if looksNumericHost(hostname) || !validASCIIDNSName(hostname) {
		return false
	}
	port := parsed.Port()
	authority := hostname
	if port != "" {
		parsedPort, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || parsedPort == 0 || parsedPort == 443 || strconv.FormatUint(parsedPort, 10) != port {
			return false
		}
		authority += ":" + port
	}
	return parsed.Host == authority && raw == "https://"+authority
}

func looksNumericHost(host string) bool {
	for _, character := range host {
		if character != '.' && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validASCIIDNSName(host string) bool {
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character < 'a' || character > 'z' {
				if character < '0' || character > '9' {
					if character != '-' {
						return false
					}
				}
			}
		}
	}
	return true
}

type builderSafeError struct {
	operation string
	domain    error
	cause     error
}

func (e *builderSafeError) Error() string { return e.operation + " failed" }
func (e *builderSafeError) Unwrap() []error {
	result := make([]error, 0, 2)
	if e.domain != nil {
		result = append(result, e.domain)
	}
	if e.cause != nil {
		result = append(result, e.cause)
	}
	return result
}

func builderDomain(operation string, domain error) error {
	return &builderSafeError{operation: operation, domain: domain}
}

func builderDependency(operation string, cause error) error {
	return &builderSafeError{operation: operation, domain: ErrDependencyUnavailable, cause: cause}
}
