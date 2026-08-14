package builder

import (
	"errors"
	"unicode/utf8"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/buildtarget"
)

const (
	maxBuilderTokenRunes = 4096
)

var (
	ErrInvalidConfig         = errors.New("builder configuration is invalid")
	ErrNotFound              = errors.New("builder configuration not found")
	ErrConflict              = errors.New("builder configuration conflict")
	ErrDisabled              = errors.New("builder is disabled")
	ErrDependencyUnavailable = errors.New("builder dependency unavailable")
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
	if !buildtarget.Valid(buildtarget.Snapshot{Name: input.Name, BaseURL: input.BaseURL, Username: input.Username, JobName: input.JobName}) ||
		!utf8.ValidString(input.Token) || utf8.RuneCountInString(input.Token) > maxBuilderTokenRunes {
		return builderDomain("validate builder configuration", ErrInvalidConfig)
	}
	return nil
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
