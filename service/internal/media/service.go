package media

import (
	"context"
	"errors"
	"path"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
)

const (
	maximumMediaSize        = 10 * 1024 * 1024
	maximumMediaDimension   = 12000
	maximumMediaNameLength  = 255
	maximumPublicKeyRetries = 5
)

var (
	mediaPublicKeyPattern = regexp.MustCompile(`^m_[a-z0-9_-]{22}$`)
	allowedExtensions     = map[string]map[string]struct{}{
		"image/jpeg": {".jpg": {}, ".jpeg": {}},
		"image/png":  {".png": {}},
		"image/webp": {".webp": {}},
		"image/gif":  {".gif": {}},
	}
)

type Service interface {
	IssueUploadPolicy(context.Context) (UploadPolicy, error)
	Register(context.Context, int64, string) (Media, error)
	ResolveReferences(context.Context, *int64, []string) (*Media, []Reference, error)
	RequireActive(context.Context, int64) error
	FindActiveByPublicKey(context.Context, string) (Media, error)
}

type service struct {
	repository Repository
	metadata   MetadataReader
	signer     *GFSSigner
	keys       *randomkey.Generator
	now        func() time.Time
}

func NewService(repository Repository, metadata MetadataReader, signer *GFSSigner, keys *randomkey.Generator, now func() time.Time) (Service, error) {
	if nilMediaDependency(repository) {
		return nil, errors.New("media repository is required")
	}
	if nilMediaDependency(metadata) {
		return nil, errors.New("media metadata reader is required")
	}
	if signer == nil || !signer.valid() {
		return nil, errors.New("media GFS signer is required")
	}
	if keys == nil {
		return nil, errors.New("media random key generator is required")
	}
	if now == nil {
		return nil, errors.New("media clock is required")
	}
	return &service{repository: repository, metadata: metadata, signer: signer, keys: keys, now: now}, nil
}

func (s *service) IssueUploadPolicy(ctx context.Context) (UploadPolicy, error) {
	if err := s.validate(ctx); err != nil {
		return UploadPolicy{}, err
	}
	policy, err := s.signer.UploadPolicy(s.now().UTC())
	if err != nil {
		return UploadPolicy{}, mediaDependencyError("issue media upload policy", err)
	}
	return policy, nil
}

func (s *service) Register(ctx context.Context, gfsFileID int64, originalName string) (Media, error) {
	if err := s.validate(ctx); err != nil {
		return Media{}, err
	}
	if gfsFileID <= 0 || !validMediaName(originalName) {
		return Media{}, ErrInvalidMetadata
	}
	existing, err := s.repository.FindByGFSFileID(ctx, gfsFileID)
	if err == nil {
		if !matchesRegistrationReplay(existing, gfsFileID, originalName) {
			return Media{}, ErrInvalidMetadata
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Media{}, mediaDependencyError("find media by GFS file ID", err)
	}

	metadata, err := s.metadata.Metadata(ctx, gfsFileID)
	if err != nil {
		return Media{}, mediaDependencyError("read media metadata", err)
	}
	if err := validateMetadata(gfsFileID, originalName, metadata); err != nil {
		return Media{}, err
	}
	at := s.now().UTC()
	for attempt := 0; attempt <= maximumPublicKeyRetries; attempt++ {
		publicKey, keyErr := s.keys.MediaPublicKey()
		if keyErr != nil {
			return Media{}, mediaDependencyError("generate media public key", keyErr)
		}
		created, createErr := s.repository.Create(ctx, NewMedia{
			PublicKey: publicKey, GFSFileID: metadata.FileID, OriginalName: metadata.FileName,
			MIMEType: metadata.ContentType, FileSize: metadata.FileSize,
			Width: metadata.Width, Height: metadata.Height,
		}, at)
		switch {
		case createErr == nil:
			return created, nil
		case errors.Is(createErr, ErrPublicKeyConflict):
			continue
		case errors.Is(createErr, ErrGFSFileIDConflict):
			concurrent, findErr := s.repository.FindByGFSFileID(ctx, gfsFileID)
			if findErr != nil {
				return Media{}, mediaDependencyError("find concurrently registered media", findErr)
			}
			if !matchesVerifiedMetadata(concurrent, metadata) {
				return Media{}, ErrInvalidMetadata
			}
			return concurrent, nil
		default:
			return Media{}, mediaDependencyError("create media", createErr)
		}
	}
	return Media{}, mediaDomainError("create media after public key retries", ErrPublicKeyConflict, nil)
}

func matchesRegistrationReplay(existing Media, gfsFileID int64, originalName string) bool {
	return existing.GFSFileID == gfsFileID && existing.OriginalName == originalName
}

func matchesVerifiedMetadata(existing Media, metadata Metadata) bool {
	return existing.GFSFileID == metadata.FileID &&
		existing.OriginalName == metadata.FileName &&
		existing.MIMEType == metadata.ContentType &&
		existing.FileSize == metadata.FileSize &&
		existing.Width == metadata.Width &&
		existing.Height == metadata.Height &&
		existing.State == "active"
}

func (s *service) ResolveReferences(ctx context.Context, coverID *int64, publicKeys []string) (*Media, []Reference, error) {
	if err := s.validate(ctx); err != nil {
		return nil, nil, err
	}
	var cover *Media
	if coverID != nil {
		if *coverID <= 0 {
			return nil, nil, ErrInvalidMetadata
		}
		item, err := s.repository.FindActiveByID(ctx, *coverID)
		if err != nil {
			return nil, nil, mediaLookupError("find active cover media", err)
		}
		cover = &item
	}

	uniqueKeys := make([]string, 0, len(publicKeys))
	seen := make(map[string]struct{}, len(publicKeys))
	for _, publicKey := range publicKeys {
		if !mediaPublicKeyPattern.MatchString(publicKey) {
			return nil, nil, ErrInvalidMetadata
		}
		if _, exists := seen[publicKey]; exists {
			continue
		}
		seen[publicKey] = struct{}{}
		uniqueKeys = append(uniqueKeys, publicKey)
	}

	stored := make([]Media, 0)
	if len(uniqueKeys) > 0 {
		var err error
		stored, err = s.repository.FindActiveByPublicKeys(ctx, uniqueKeys)
		if err != nil {
			return nil, nil, mediaLookupError("find active content media", err)
		}
	}
	byKey := make(map[string]Media, len(stored))
	for _, item := range stored {
		byKey[item.PublicKey] = item
	}
	if len(byKey) != len(uniqueKeys) {
		return nil, nil, ErrNotFound
	}

	references := make([]Reference, 0, len(uniqueKeys)+1)
	position := 0
	if cover != nil {
		references = append(references, Reference{MediaID: cover.ID, PublicKey: cover.PublicKey, Purpose: "cover", Position: position})
		position++
	}
	for _, publicKey := range uniqueKeys {
		item, exists := byKey[publicKey]
		if !exists {
			return nil, nil, ErrNotFound
		}
		references = append(references, Reference{MediaID: item.ID, PublicKey: item.PublicKey, Purpose: "content", Position: position})
		position++
	}
	return cover, references, nil
}

func (s *service) RequireActive(ctx context.Context, id int64) error {
	if err := s.validate(ctx); err != nil {
		return err
	}
	if id <= 0 {
		return ErrInvalidMetadata
	}
	_, err := s.repository.FindActiveByID(ctx, id)
	if err != nil {
		return mediaLookupError("require active media", err)
	}
	return nil
}

func (s *service) FindActiveByPublicKey(ctx context.Context, publicKey string) (Media, error) {
	if err := s.validate(ctx); err != nil {
		return Media{}, err
	}
	if !mediaPublicKeyPattern.MatchString(publicKey) {
		return Media{}, ErrInvalidMetadata
	}
	item, err := s.repository.FindActiveByPublicKey(ctx, publicKey)
	if err != nil {
		return Media{}, mediaLookupError("find active media by public key", err)
	}
	return item, nil
}

func (s *service) validate(ctx context.Context) error {
	if s == nil || nilMediaDependency(s.repository) || nilMediaDependency(s.metadata) || s.signer == nil || s.keys == nil || s.now == nil || nilMediaDependency(ctx) {
		return ErrDependencyUnavailable
	}
	return nil
}

func validMediaName(name string) bool {
	return name != "" && len(name) <= maximumMediaNameLength && name != "." && name != ".." &&
		path.Base(name) == name && !strings.ContainsAny(name, "\\\x00")
}

func validateMetadata(requestedID int64, originalName string, metadata Metadata) error {
	if requestedID <= 0 || metadata.FileID <= 0 || metadata.FileID != requestedID || metadata.FileName != originalName || !validMediaName(metadata.FileName) ||
		metadata.FileSize <= 0 || metadata.FileSize > maximumMediaSize ||
		metadata.Width <= 0 || metadata.Width > maximumMediaDimension ||
		metadata.Height <= 0 || metadata.Height > maximumMediaDimension {
		return ErrInvalidMetadata
	}
	extensions, allowed := allowedExtensions[metadata.ContentType]
	if !allowed {
		return ErrInvalidMetadata
	}
	if _, allowed = extensions[strings.ToLower(path.Ext(metadata.FileName))]; !allowed {
		return ErrInvalidMetadata
	}
	return nil
}

func mediaLookupError(operation string, err error) error {
	if errors.Is(err, ErrNotFound) {
		return mediaDomainError(operation, ErrNotFound, err)
	}
	return mediaDependencyError(operation, err)
}

func mediaDependencyError(operation string, cause error) error {
	return mediaDomainError(operation, ErrDependencyUnavailable, cause)
}

type mediaError struct {
	operation string
	domain    error
	cause     error
}

func (e *mediaError) Error() string { return e.operation + " failed" }
func (e *mediaError) Unwrap() []error {
	if e.cause == nil {
		return []error{e.domain}
	}
	return []error{e.domain, e.cause}
}

func mediaDomainError(operation string, domain, cause error) error {
	return &mediaError{operation: operation, domain: domain, cause: cause}
}

func nilMediaDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

var _ Service = (*service)(nil)
