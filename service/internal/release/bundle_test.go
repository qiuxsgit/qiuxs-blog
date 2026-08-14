package release

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

func TestCanonicalBundleIsDeterministicAndChecksumCoversOnlyPublishedPayload(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 123000000, time.UTC)
	prepared := validPreparedSnapshot(now)
	prepared.Checksum = ""
	prepared.Articles = append(prepared.Articles, ArticleSnapshot{
		ArticleID: 12, RevisionID: 13, Slug: "second_slug_", Title: "Second", Summary: "",
		ContentMarkdown: "Second body", ContentHash: "sha256:" + strings.Repeat("c", 64), PublishedAt: now.Add(-time.Hour),
		Tags: []TagSnapshot{{ID: 9, Name: "数据库", Slug: "t_mnopqrstuvwx"}, {ID: 5, Name: "Go", Slug: "t_abcdefghijkl"}},
	})

	bundle, err := assembleBundle(7, now.Add(time.Minute), prepared)
	require.NoError(t, err)
	encoded, etag, err := encodeCanonicalBundle(bundle)
	require.NoError(t, err)
	require.Equal(t, bundle.Checksum, etag)
	require.Equal(t, "sha256:8bb94eb6793dbc6178fbf5c6eef336389e2f1d71848ad448a95e14b77324f861", etag)
	require.Equal(t, `{"articles":[{"articleId":12,"contentHash":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","contentMarkdown":"Second body","publishedAt":"2026-08-14T11:00:00.123Z","revisionId":13,"slug":"second_slug_","summary":"","tags":["t_abcdefghijkl","t_mnopqrstuvwx"],"title":"Second"},{"articleId":41,"contentHash":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","contentMarkdown":"Body","publishedAt":"2026-08-14T12:00:00.123Z","revisionId":71,"slug":"article_slug","summary":"Summary","tags":["t_abcdefghijkl"],"title":"Title"}],"checksum":"sha256:8bb94eb6793dbc6178fbf5c6eef336389e2f1d71848ad448a95e14b77324f861","generatedAt":"2026-08-14T12:01:00.123Z","releaseId":7,"schemaVersion":1,"site":{"aboutMarkdown":"About","authorBio":"Bio","filingName":"ICP","filingNumber":"ICP-1","name":"Blog","socialLinks":[]},"tags":[{"id":5,"name":"Go","slug":"t_abcdefghijkl"},{"id":9,"name":"数据库","slug":"t_mnopqrstuvwx"}]}`, string(encoded))

	// Release identity and generation time are deliberately outside the
	// checksum payload, while remaining part of the full immutable bytes.
	other, err := assembleBundle(99, now.Add(2*time.Minute), prepared)
	require.NoError(t, err)
	require.Equal(t, bundle.Checksum, other.Checksum)
	otherBytes, _, err := encodeCanonicalBundle(other)
	require.NoError(t, err)
	require.NotEqual(t, encoded, otherBytes)

	// Caller-owned values cannot mutate a built bundle, and source ordering has
	// no effect on its bytes.
	prepared.Site.Name = "mutated"
	prepared.Articles[0].Tags[0].Name = "mutated"
	reencoded, _, err := encodeCanonicalBundle(bundle)
	require.NoError(t, err)
	require.Equal(t, encoded, reencoded)

	reordered := validPreparedSnapshot(now)
	reordered.Checksum = ""
	reordered.Articles = []ArticleSnapshot{prepared.Articles[1], validPreparedSnapshot(now).Articles[0]}
	reordered.Articles[0].Tags[0], reordered.Articles[0].Tags[1] = reordered.Articles[0].Tags[1], reordered.Articles[0].Tags[0]
	reorderedBundle, err := assembleBundle(7, now.Add(time.Minute), reordered)
	require.NoError(t, err)
	reorderedBytes, _, err := encodeCanonicalBundle(reorderedBundle)
	require.NoError(t, err)
	require.Equal(t, encoded, reorderedBytes)
}

func TestCanonicalBundleValidatesPublishedSchemaAndRejectsStoredChecksumMismatch(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	prepared := validPreparedSnapshot(now)
	prepared.Checksum = ""
	bundle, err := assembleBundle(7, now, prepared)
	require.NoError(t, err)
	encoded, _, err := encodeCanonicalBundle(bundle)
	require.NoError(t, err)

	schemaFile, err := os.ReadFile("../../../contracts/release-bundle-v1.schema.json")
	require.NoError(t, err)
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaFile))
	require.NoError(t, err)
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	require.NoError(t, compiler.AddResource("release-bundle-v1.schema.json", document))
	schema, err := compiler.Compile("release-bundle-v1.schema.json")
	require.NoError(t, err)
	var value any
	require.NoError(t, json.Unmarshal(encoded, &value))
	require.NoError(t, schema.Validate(value))

	bundle.Checksum = "sha256:" + strings.Repeat("a", 64)
	_, _, err = encodeCanonicalBundle(bundle)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
	require.NotContains(t, err.Error(), bundle.Checksum)
}

func TestPreparedSnapshotChecksumIsIndependentAndNilSafe(t *testing.T) {
	prepared := validPreparedSnapshot(time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	checksum, err := preparedSnapshotChecksum(prepared)
	require.NoError(t, err)
	require.NotEmpty(t, checksum)

	prepared.Checksum = "sha256:" + strings.Repeat("f", 64)
	require.Equal(t, checksum, mustPreparedChecksum(t, prepared))
	_, err = preparedSnapshotChecksum(PreparedSnapshot{})
	require.ErrorIs(t, err, ErrInvalidSnapshot)

}

func mustPreparedChecksum(t *testing.T, prepared PreparedSnapshot) string {
	t.Helper()
	checksum, err := preparedSnapshotChecksum(prepared)
	require.NoError(t, err)
	return checksum
}
