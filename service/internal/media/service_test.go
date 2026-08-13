package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qiuxsgit/qiuxs-blog/service/internal/randomkey"
	"github.com/stretchr/testify/require"
)

func TestServiceIssueUploadPolicyUsesInjectedClockAndFixedSignerPath(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 34, 56, 0, time.FixedZone("CST", 8*60*60))
	repository := newMediaRepositoryFake()
	metadata := &metadataReaderFake{}
	keys := newMediaKeys(t, bytes.Repeat([]byte{0}, 128))
	signer, err := NewGFSSigner("https://gfs.example.com", "blog-app", "raw-secret", "read-secret", keys)
	require.NoError(t, err)
	service, err := NewService(repository, metadata, signer, keys, func() time.Time { return now })
	require.NoError(t, err)

	policy, err := service.IssueUploadPolicy(context.Background())

	require.NoError(t, err)
	require.Equal(t, "https://gfs.example.com/v1/upload", policy.UploadURL)
	require.Equal(t, "1786595696", policy.Timestamp)
	decodedPolicy := decodePolicy(t, policy.Policy)
	require.Equal(t, map[string]string{"savePath": "blog/{{year}}/{{month}}/{{uuid}}.{{fileExt}}"}, decodedPolicy)
}

func TestServiceRegisterAcceptsOnlyVerifiedImageMetadata(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		contentType string
	}{
		{name: "JPEG jpg", filename: "photo.jpg", contentType: "image/jpeg"},
		{name: "JPEG jpeg uppercase extension", filename: "photo.JPEG", contentType: "image/jpeg"},
		{name: "PNG", filename: "photo.png", contentType: "image/png"},
		{name: "WebP", filename: "photo.webp", contentType: "image/webp"},
		{name: "GIF", filename: "photo.gif", contentType: "image/gif"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newMediaRepositoryFake()
			metadata := &metadataReaderFake{result: Metadata{
				FileID: 91, FileName: test.filename, ContentType: test.contentType,
				FileSize: 2048, Width: 640, Height: 480,
			}}
			service := newMediaService(t, repository, metadata, bytes.Repeat([]byte{0}, 128))

			got, err := service.Register(context.Background(), 91, test.filename)

			require.NoError(t, err)
			require.Equal(t, int64(91), got.GFSFileID)
			require.Equal(t, "m_aaaaaaaaaaaaaaaaaaaaaa", got.PublicKey)
			require.Equal(t, test.filename, got.OriginalName)
			require.Equal(t, test.contentType, got.MIMEType)
			require.Equal(t, int64(2048), got.FileSize)
			require.Equal(t, 640, got.Width)
			require.Equal(t, 480, got.Height)
			require.Equal(t, "active", got.State)
			require.Len(t, repository.creates, 1)
			require.Equal(t, test.filename, repository.creates[0].OriginalName)
			require.Equal(t, test.contentType, repository.creates[0].MIMEType)
			require.Equal(t, []int64{91}, metadata.calls)
		})
	}
}

func TestServiceRegisterRejectsInvalidCallerAndMetadataValues(t *testing.T) {
	valid := Metadata{FileID: 91, FileName: "photo.png", ContentType: "image/png", FileSize: 2048, Width: 640, Height: 480}
	tests := []struct {
		name       string
		gfsFileID  int64
		callerName string
		mutate     func(*Metadata)
	}{
		{name: "zero file ID", gfsFileID: 0, callerName: "photo.png"},
		{name: "negative file ID", gfsFileID: -1, callerName: "photo.png"},
		{name: "empty filename", gfsFileID: 91, callerName: ""},
		{name: "dot filename", gfsFileID: 91, callerName: "."},
		{name: "slash path", gfsFileID: 91, callerName: "folder/photo.png"},
		{name: "backslash path", gfsFileID: 91, callerName: `folder\photo.png`},
		{name: "NUL filename", gfsFileID: 91, callerName: "photo\x00.png"},
		{name: "filename mismatch", gfsFileID: 91, callerName: "photo.png", mutate: func(value *Metadata) { value.FileName = "other.png" }},
		{name: "response ID mismatch", gfsFileID: 91, callerName: "photo.png", mutate: func(value *Metadata) { value.FileID = 92 }},
		{name: "SVG", gfsFileID: 91, callerName: "photo.svg", mutate: func(value *Metadata) { value.FileName, value.ContentType = "photo.svg", "image/svg+xml" }},
		{name: "extension MIME mismatch", gfsFileID: 91, callerName: "photo.png", mutate: func(value *Metadata) { value.ContentType = "image/jpeg" }},
		{name: "zero size", gfsFileID: 91, callerName: "photo.png", mutate: func(value *Metadata) { value.FileSize = 0 }},
		{name: "negative size", gfsFileID: 91, callerName: "photo.png", mutate: func(value *Metadata) { value.FileSize = -1 }},
		{name: "over 10 MiB", gfsFileID: 91, callerName: "photo.png", mutate: func(value *Metadata) { value.FileSize = 10*1024*1024 + 1 }},
		{name: "zero width", gfsFileID: 91, callerName: "photo.png", mutate: func(value *Metadata) { value.Width = 0 }},
		{name: "negative height", gfsFileID: 91, callerName: "photo.png", mutate: func(value *Metadata) { value.Height = -1 }},
		{name: "over max width", gfsFileID: 91, callerName: "photo.png", mutate: func(value *Metadata) { value.Width = 12001 }},
		{name: "over max height", gfsFileID: 91, callerName: "photo.png", mutate: func(value *Metadata) { value.Height = 12001 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newMediaRepositoryFake()
			actual := valid
			actual.FileName = test.callerName
			if test.mutate != nil {
				test.mutate(&actual)
			}
			metadata := &metadataReaderFake{result: actual}
			service := newMediaService(t, repository, metadata, bytes.Repeat([]byte{0}, 128))

			_, err := service.Register(context.Background(), test.gfsFileID, test.callerName)

			require.ErrorIs(t, err, ErrInvalidMetadata)
			require.Empty(t, repository.creates)
			if test.gfsFileID <= 0 || test.callerName == "" || test.callerName == "." || strings.ContainsAny(test.callerName, "/\\\x00") {
				require.Empty(t, metadata.calls)
			}
		})
	}
}

func TestServiceRegisterMapsMetadataDependencyFailureWithoutLeak(t *testing.T) {
	repository := newMediaRepositoryFake()
	metadata := &metadataReaderFake{err: errors.New("gfs-body-and-url-secret")}
	service := newMediaService(t, repository, metadata, bytes.Repeat([]byte{0}, 128))

	_, err := service.Register(context.Background(), 91, "photo.png")

	require.ErrorIs(t, err, ErrDependencyUnavailable)
	require.NotContains(t, err.Error(), "secret")
	require.Empty(t, repository.creates)
}

func TestServiceRegisterReturnsExistingGFSFileWithoutMetadataOrInsert(t *testing.T) {
	existing := Media{ID: 31, PublicKey: "m_existingaaaaaaaaaaaaaa", GFSFileID: 91, OriginalName: "photo.png", State: "active"}
	repository := newMediaRepositoryFake()
	repository.byGFSFileID[91] = existing
	metadata := &metadataReaderFake{err: errors.New("must not be called")}
	service := newMediaService(t, repository, metadata, bytes.Repeat([]byte{0}, 128))

	got, err := service.Register(context.Background(), 91, "photo.png")

	require.NoError(t, err)
	require.Equal(t, existing, got)
	require.Empty(t, metadata.calls)
	require.Empty(t, repository.creates)
}

func TestServiceRegisterRetriesOnlyPublicKeyConflictFiveTimes(t *testing.T) {
	valid := Metadata{FileID: 91, FileName: "photo.png", ContentType: "image/png", FileSize: 2048, Width: 640, Height: 480}
	t.Run("success after one conflict", func(t *testing.T) {
		repository := newMediaRepositoryFake()
		repository.createErrors = []error{ErrPublicKeyConflict, nil}
		service := newMediaService(t, repository, &metadataReaderFake{result: valid}, append(bytes.Repeat([]byte{0}, 22), bytes.Repeat([]byte{1}, 22)...))

		got, err := service.Register(context.Background(), 91, "photo.png")

		require.NoError(t, err)
		require.Equal(t, "m_bbbbbbbbbbbbbbbbbbbbbb", got.PublicKey)
		require.Len(t, repository.creates, 2)
	})

	t.Run("stops after five retries", func(t *testing.T) {
		repository := newMediaRepositoryFake()
		repository.createErrors = []error{ErrPublicKeyConflict, ErrPublicKeyConflict, ErrPublicKeyConflict, ErrPublicKeyConflict, ErrPublicKeyConflict, ErrPublicKeyConflict}
		service := newMediaService(t, repository, &metadataReaderFake{result: valid}, bytes.Repeat([]byte{0}, 22*6))

		_, err := service.Register(context.Background(), 91, "photo.png")

		require.ErrorIs(t, err, ErrPublicKeyConflict)
		require.Len(t, repository.creates, 6)
	})

	t.Run("dependency failure is not retried", func(t *testing.T) {
		repository := newMediaRepositoryFake()
		repository.createErrors = []error{errors.New("mysql-secret")}
		service := newMediaService(t, repository, &metadataReaderFake{result: valid}, bytes.Repeat([]byte{0}, 128))

		_, err := service.Register(context.Background(), 91, "photo.png")

		require.NotContains(t, err.Error(), "mysql-secret")
		require.Len(t, repository.creates, 1)
	})
}

func TestServiceRegisterRecoversConcurrentGFSFileConflict(t *testing.T) {
	existing := Media{ID: 31, PublicKey: "m_existingaaaaaaaaaaaaaa", GFSFileID: 91, OriginalName: "photo.png", State: "active"}
	repository := newMediaRepositoryFake()
	repository.findGFSResults = []mediaResult{{err: ErrNotFound}, {item: existing}}
	repository.createErrors = []error{ErrGFSFileIDConflict}
	metadata := &metadataReaderFake{result: Metadata{FileID: 91, FileName: "photo.png", ContentType: "image/png", FileSize: 2048, Width: 640, Height: 480}}
	service := newMediaService(t, repository, metadata, bytes.Repeat([]byte{0}, 128))

	got, err := service.Register(context.Background(), 91, "photo.png")

	require.NoError(t, err)
	require.Equal(t, existing, got)
	require.Len(t, repository.creates, 1)
}

func TestServiceResolveReferencesRequiresActiveMediaAndPreservesFirstUseOrder(t *testing.T) {
	coverID := int64(7)
	cover := Media{ID: 7, PublicKey: "m_0000000000000000000001", State: "active"}
	first := Media{ID: 11, PublicKey: "m_0000000000000000000002", State: "active"}
	second := Media{ID: 12, PublicKey: "m_0000000000000000000003", State: "active"}
	repository := newMediaRepositoryFake()
	repository.byActiveID[coverID] = cover
	repository.activePublicKeyResults = []Media{second, first}
	service := newMediaService(t, repository, &metadataReaderFake{}, bytes.Repeat([]byte{0}, 128))

	gotCover, references, err := service.ResolveReferences(context.Background(), &coverID, []string{first.PublicKey, second.PublicKey, first.PublicKey})

	require.NoError(t, err)
	require.Equal(t, cover, *gotCover)
	require.Equal(t, []Reference{
		{MediaID: 7, PublicKey: cover.PublicKey, Purpose: "cover", Position: 0},
		{MediaID: 11, PublicKey: first.PublicKey, Purpose: "content", Position: 1},
		{MediaID: 12, PublicKey: second.PublicKey, Purpose: "content", Position: 2},
	}, references)
	require.Equal(t, []string{first.PublicKey, second.PublicKey}, repository.findActivePublicKeyCalls[0])
}

func TestServiceResolveReferencesUsesZeroBasedBodyPositionsWithoutCover(t *testing.T) {
	item := Media{ID: 11, PublicKey: "m_0000000000000000000002", State: "active"}
	repository := newMediaRepositoryFake()
	repository.activePublicKeyResults = []Media{item}
	service := newMediaService(t, repository, &metadataReaderFake{}, bytes.Repeat([]byte{0}, 128))

	cover, references, err := service.ResolveReferences(context.Background(), nil, []string{item.PublicKey})

	require.NoError(t, err)
	require.Nil(t, cover)
	require.Equal(t, []Reference{{MediaID: 11, PublicKey: item.PublicKey, Purpose: "content", Position: 0}}, references)
}

func TestServiceResolveReferencesRejectsMissingInactiveAndNumericReferencesAtomically(t *testing.T) {
	tests := []struct {
		name      string
		coverID   *int64
		keys      []string
		configure func(*mediaRepositoryFake)
		want      error
	}{
		{name: "missing cover", coverID: int64Pointer(7), configure: func(repository *mediaRepositoryFake) {}, want: ErrNotFound},
		{name: "missing body key", keys: []string{"m_0000000000000000000005"}, configure: func(repository *mediaRepositoryFake) {}, want: ErrNotFound},
		{name: "inactive body key omitted by repository", keys: []string{"m_0000000000000000000006"}, configure: func(repository *mediaRepositoryFake) {}, want: ErrNotFound},
		{name: "numeric GFS ID", keys: []string{"91"}, configure: func(repository *mediaRepositoryFake) {}, want: ErrInvalidMetadata},
		{name: "invalid cover ID", coverID: int64Pointer(0), configure: func(repository *mediaRepositoryFake) {}, want: ErrInvalidMetadata},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newMediaRepositoryFake()
			test.configure(repository)
			service := newMediaService(t, repository, &metadataReaderFake{}, bytes.Repeat([]byte{0}, 128))

			cover, references, err := service.ResolveReferences(context.Background(), test.coverID, test.keys)

			require.ErrorIs(t, err, test.want)
			require.Nil(t, cover)
			require.Nil(t, references)
		})
	}
}

func TestServiceRequireAndFindActiveDelegateSafely(t *testing.T) {
	item := Media{ID: 7, PublicKey: "m_0000000000000000000004", State: "active"}
	repository := newMediaRepositoryFake()
	repository.byActiveID[item.ID] = item
	repository.byActivePublicKey[item.PublicKey] = item
	service := newMediaService(t, repository, &metadataReaderFake{}, bytes.Repeat([]byte{0}, 128))

	require.NoError(t, service.RequireActive(context.Background(), item.ID))
	got, err := service.FindActiveByPublicKey(context.Background(), item.PublicKey)
	require.NoError(t, err)
	require.Equal(t, item, got)

	require.ErrorIs(t, service.RequireActive(context.Background(), 0), ErrInvalidMetadata)
	_, err = service.FindActiveByPublicKey(context.Background(), "91")
	require.ErrorIs(t, err, ErrInvalidMetadata)
}

func TestNewServiceRejectsNilDependenciesAndMethodsAreNilSafe(t *testing.T) {
	repository := newMediaRepositoryFake()
	metadata := &metadataReaderFake{}
	keys := newMediaKeys(t, bytes.Repeat([]byte{0}, 128))
	signer, err := NewGFSSigner("https://gfs.example.com", "blog-app", "raw-secret", "read-secret", keys)
	require.NoError(t, err)
	var typedNilRepository *mediaRepositoryFake
	var typedNilMetadata *metadataReaderFake

	for _, test := range []struct {
		name       string
		repository Repository
		metadata   MetadataReader
		signer     *GFSSigner
		keys       *randomkey.Generator
		now        func() time.Time
	}{
		{name: "nil repository", metadata: metadata, signer: signer, keys: keys, now: time.Now},
		{name: "typed nil repository", repository: typedNilRepository, metadata: metadata, signer: signer, keys: keys, now: time.Now},
		{name: "nil metadata", repository: repository, signer: signer, keys: keys, now: time.Now},
		{name: "typed nil metadata", repository: repository, metadata: typedNilMetadata, signer: signer, keys: keys, now: time.Now},
		{name: "nil signer", repository: repository, metadata: metadata, keys: keys, now: time.Now},
		{name: "nil keys", repository: repository, metadata: metadata, signer: signer, now: time.Now},
		{name: "nil clock", repository: repository, metadata: metadata, signer: signer, keys: keys},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewService(test.repository, test.metadata, test.signer, test.keys, test.now)
			require.Nil(t, service)
			require.Error(t, err)
		})
	}

	var nilService *service
	require.NotPanics(t, func() {
		_, err = nilService.Register(context.Background(), 91, "photo.png")
	})
	require.Error(t, err)
	valid := newMediaService(t, repository, metadata, bytes.Repeat([]byte{0}, 128))
	require.NotPanics(t, func() {
		_, err = valid.IssueUploadPolicy(nil)
	})
	require.Error(t, err)
}

type mediaResult struct {
	item Media
	err  error
}

type mediaRepositoryFake struct {
	byGFSFileID              map[int64]Media
	byActiveID               map[int64]Media
	byActivePublicKey        map[string]Media
	findGFSResults           []mediaResult
	activePublicKeyResults   []Media
	createErrors             []error
	creates                  []NewMedia
	findActivePublicKeyCalls [][]string
}

func newMediaRepositoryFake() *mediaRepositoryFake {
	return &mediaRepositoryFake{
		byGFSFileID:       make(map[int64]Media),
		byActiveID:        make(map[int64]Media),
		byActivePublicKey: make(map[string]Media),
	}
}

func (r *mediaRepositoryFake) Create(_ context.Context, input NewMedia, at time.Time) (Media, error) {
	r.creates = append(r.creates, input)
	index := len(r.creates) - 1
	if index < len(r.createErrors) && r.createErrors[index] != nil {
		return Media{}, r.createErrors[index]
	}
	return Media{
		ID: int64(index + 1), PublicKey: input.PublicKey, GFSFileID: input.GFSFileID,
		OriginalName: input.OriginalName, MIMEType: input.MIMEType, FileSize: input.FileSize,
		Width: input.Width, Height: input.Height, State: "active", CreatedAt: at, UpdatedAt: at,
	}, nil
}

func (r *mediaRepositoryFake) FindByGFSFileID(_ context.Context, id int64) (Media, error) {
	if len(r.findGFSResults) > 0 {
		result := r.findGFSResults[0]
		r.findGFSResults = r.findGFSResults[1:]
		return result.item, result.err
	}
	item, exists := r.byGFSFileID[id]
	if !exists {
		return Media{}, ErrNotFound
	}
	return item, nil
}

func (r *mediaRepositoryFake) FindActiveByID(_ context.Context, id int64) (Media, error) {
	item, exists := r.byActiveID[id]
	if !exists {
		return Media{}, ErrNotFound
	}
	return item, nil
}

func (r *mediaRepositoryFake) FindActiveByIDs(_ context.Context, ids []int64) ([]Media, error) {
	items := make([]Media, 0, len(ids))
	for _, id := range ids {
		if item, exists := r.byActiveID[id]; exists {
			items = append(items, item)
		}
	}
	return items, nil
}

func (r *mediaRepositoryFake) FindActiveByPublicKeys(_ context.Context, keys []string) ([]Media, error) {
	r.findActivePublicKeyCalls = append(r.findActivePublicKeyCalls, append([]string(nil), keys...))
	return append([]Media(nil), r.activePublicKeyResults...), nil
}

func (r *mediaRepositoryFake) FindActiveByPublicKey(_ context.Context, key string) (Media, error) {
	item, exists := r.byActivePublicKey[key]
	if !exists {
		return Media{}, ErrNotFound
	}
	return item, nil
}

type metadataReaderFake struct {
	result Metadata
	err    error
	calls  []int64
}

func (r *metadataReaderFake) Metadata(_ context.Context, id int64) (Metadata, error) {
	r.calls = append(r.calls, id)
	return r.result, r.err
}

func newMediaService(t *testing.T, repository Repository, metadata MetadataReader, random []byte) Service {
	t.Helper()
	keys := newMediaKeys(t, random)
	signer, err := NewGFSSigner("https://gfs.example.com", "blog-app", "raw-secret", "read-secret", keys)
	require.NoError(t, err)
	service, err := NewService(repository, metadata, signer, keys, func() time.Time {
		return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	})
	require.NoError(t, err)
	return service
}

func newMediaKeys(t *testing.T, random []byte) *randomkey.Generator {
	t.Helper()
	keys, err := randomkey.New(bytes.NewReader(random))
	require.NoError(t, err)
	return keys
}

func int64Pointer(value int64) *int64 { return &value }

func decodePolicy(t *testing.T, encoded string) map[string]string {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var policy map[string]string
	require.NoError(t, json.Unmarshal(decoded, &policy))
	return policy
}
