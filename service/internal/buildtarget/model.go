package buildtarget

import (
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxNameRunes     = 100
	MaxUsernameRunes = 255
	MaxJobBytes      = 128
)

var jobNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

type Snapshot struct {
	Name     string
	BaseURL  string
	Username string
	JobName  string
}

func Valid(value Snapshot) bool {
	return validTrimmed(value.Name, MaxNameRunes) &&
		validTrimmed(value.Username, MaxUsernameRunes) && !strings.Contains(value.Username, ":") &&
		canonicalJenkinsOrigin(value.BaseURL) && validJobName(value.JobName)
}

func validTrimmed(value string, maximum int) bool {
	return utf8.ValidString(value) && value != "" && value == strings.TrimSpace(value) && utf8.RuneCountInString(value) <= maximum
}

func validJobName(value string) bool {
	if len(value) == 0 || len(value) > MaxJobBytes || !utf8.ValidString(value) || !jobNamePattern.MatchString(value) {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
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
