package revision

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	firstMediaKey  = "m_abcdefghijklmnopqrstuv"
	secondMediaKey = "m_0123456789-_abcdefghij"
)

func TestValidateDraftAcceptsSupportedGFM(t *testing.T) {
	content := Content{ContentMD: strings.Join([]string{
		"# Heading",
		"",
		"| Language | Stable |",
		"| --- | --- |",
		"| Go | yes |",
		"",
		"- [x] task",
		"",
		"```go",
		"fmt.Println(\"hello\")",
		"```",
		"",
		"<https://example.com>",
	}, "\n")}

	keys, err := ValidateDraft(content)

	require.NoError(t, err)
	require.Empty(t, keys)
}

func TestValidateDraftRejectsInlineAndBlockRawHTML(t *testing.T) {
	for _, markdown := range []string{
		"text <span>unsafe</span>",
		"<div>\nunsafe\n</div>\n",
	} {
		_, err := ValidateDraft(Content{ContentMD: markdown})
		require.ErrorIs(t, err, ErrInvalidContent)
	}
}

func TestValidateDraftExtractsExactRegisteredImagesInFirstUseOrder(t *testing.T) {
	markdown := strings.Join([]string{
		"![first](/img/proxy/" + firstMediaKey + ")",
		"![duplicate](/img/proxy/" + firstMediaKey + ")",
		"![external](https://qiuxs.com/img/proxy/" + firstMediaKey + ")",
		"![query](/img/proxy/" + firstMediaKey + "?size=2)",
		"![fragment](/img/proxy/" + firstMediaKey + "#x)",
		"![short](/img/proxy/m_short)",
		"![other relative](images/" + firstMediaKey + ")",
		"`![inline](/img/proxy/" + secondMediaKey + ")`",
		"```md",
		"![fenced](/img/proxy/" + secondMediaKey + ")",
		"```",
		"![second](/img/proxy/" + secondMediaKey + ")",
	}, "\n")

	keys, err := ValidateDraft(Content{ContentMD: markdown})

	require.NoError(t, err)
	require.Equal(t, []string{firstMediaKey, secondMediaKey}, keys)
}

func TestValidateDraftAllowsTransientBlobDestinationsButFreezableRejectsThem(t *testing.T) {
	for _, markdown := range []string{
		"![pending](blob:https://blog-admin.qiuxs.com/image-id)",
		"[pending](blob:https://blog-admin.qiuxs.com/link-id)",
	} {
		keys, err := ValidateDraft(Content{ContentMD: markdown})
		require.NoError(t, err)
		require.Empty(t, keys)
		require.ErrorIs(t, ValidateFreezable(Content{Title: "Publishable", ContentMD: markdown}), ErrInvalidContent)
	}
}

func TestValidateFreezableRejectsBlobDestinationsAfterGoldmarkNormalization(t *testing.T) {
	for _, test := range []struct {
		name        string
		destination string
	}{
		{name: "decimal entity", destination: "blob&#58;https://blog-admin.qiuxs.com/item"},
		{name: "hex entity", destination: "blob&#x3a;https://blog-admin.qiuxs.com/item"},
		{name: "named entity", destination: "blob&colon;https://blog-admin.qiuxs.com/item"},
		{name: "backslash escape", destination: `blob\:https://blog-admin.qiuxs.com/item`},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, markdown := range []string{
				"[pending](" + test.destination + ")",
				"![pending](" + test.destination + ")",
			} {
				err := ValidateFreezable(Content{Title: "Publishable", ContentMD: markdown})
				require.ErrorIs(t, err, ErrInvalidContent)
				require.NotContains(t, err.Error(), test.destination)
			}
		})
	}
}

func TestValidateFreezableNormalizesDestinationOnlyOnce(t *testing.T) {
	err := ValidateFreezable(Content{
		Title:     "Publishable",
		ContentMD: "[literal entity](blob&amp;#58;https://blog-admin.qiuxs.com/item)",
	})

	require.NoError(t, err)
}

func TestValidateDraftRejectsReservedProxyDelimitersEvenWhenEmpty(t *testing.T) {
	for _, destination := range []string{
		"/img/proxy/" + firstMediaKey + "?",
		"/img/proxy/" + firstMediaKey + "#",
		`/img/proxy/` + firstMediaKey + `\?`,
		"/img/proxy/" + firstMediaKey + "&#35;",
	} {
		keys, err := ValidateDraft(Content{ContentMD: "![image](" + destination + ")"})

		require.NoError(t, err)
		require.Empty(t, keys)
	}
}

func TestValidateDraftBoundsUniqueBodyMediaReferences(t *testing.T) {
	keys, err := ValidateDraft(Content{ContentMD: registeredImagesMarkdown(MaxBodyMediaCount, false)})
	require.NoError(t, err)
	require.Len(t, keys, MaxBodyMediaCount)

	_, err = ValidateDraft(Content{ContentMD: registeredImagesMarkdown(MaxBodyMediaCount+1, false)})
	require.ErrorIs(t, err, ErrInvalidContent)

	keys, err = ValidateDraft(Content{ContentMD: registeredImagesMarkdown(MaxBodyMediaCount+1, true)})
	require.NoError(t, err)
	require.Equal(t, []string{mediaKeyForIndex(0)}, keys)
}

func TestValidateFreezableRequiresNonblankTitle(t *testing.T) {
	for _, title := range []string{"", " \t\n "} {
		require.ErrorIs(t, ValidateFreezable(Content{Title: title, ContentMD: "body"}), ErrInvalidContent)
	}
	require.NoError(t, ValidateFreezable(Content{Title: " Title ", ContentMD: "body"}))
}

func TestValidateDraftEnforcesExactByteAndRuneLimits(t *testing.T) {
	for _, test := range []struct {
		name    string
		content Content
		valid   bool
	}{
		{name: "markdown exact", content: Content{ContentMD: strings.Repeat("a", 2*1024*1024)}, valid: true},
		{name: "markdown over", content: Content{ContentMD: strings.Repeat("a", 2*1024*1024+1)}},
		{name: "title exact multibyte", content: Content{Title: strings.Repeat("界", 200)}, valid: true},
		{name: "title over", content: Content{Title: strings.Repeat("界", 201)}},
		{name: "summary exact multibyte", content: Content{Summary: strings.Repeat("界", 600)}, valid: true},
		{name: "summary over", content: Content{Summary: strings.Repeat("界", 601)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateDraft(test.content)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrInvalidContent)
			}
		})
	}
}

func registeredImagesMarkdown(count int, duplicate bool) string {
	var markdown strings.Builder
	for index := 0; index < count; index++ {
		keyIndex := index
		if duplicate {
			keyIndex = 0
		}
		fmt.Fprintf(&markdown, "![image](/img/proxy/%s)\n", mediaKeyForIndex(keyIndex))
	}
	return markdown.String()
}

func mediaKeyForIndex(index int) string {
	return fmt.Sprintf("m_%022d", index)
}
