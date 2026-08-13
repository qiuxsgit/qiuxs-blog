package httpapi_test

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

const adminContractPath = "../../../contracts/openapi/admin-v1.yaml"

type operationContract struct {
	method      string
	path        string
	operationID string
	success     int
	schema      string
}

var stage2OperationContracts = []operationContract{
	{"GET", "/api/admin/v1/articles", "listArticles", 200, "ArticleList"},
	{"POST", "/api/admin/v1/articles", "createArticle", 201, "ArticleDetail"},
	{"GET", "/api/admin/v1/articles/{articleId}", "getArticle", 200, "ArticleDetail"},
	{"PUT", "/api/admin/v1/articles/{articleId}/draft", "saveArticleDraft", 200, "DraftView"},
	{"GET", "/api/admin/v1/articles/{articleId}/preview", "getArticlePreview", 200, "PreviewView"},
	{"GET", "/api/admin/v1/articles/{articleId}/versions", "listArticleVersions", 200, "RevisionList"},
	{"POST", "/api/admin/v1/articles/{articleId}/versions", "createArticleVersion", 201, "VersionResult"},
	{"POST", "/api/admin/v1/articles/{articleId}/versions/{revisionId}/restore", "restoreArticleVersion", 200, "DraftView"},
	{"POST", "/api/admin/v1/articles/{articleId}/trash", "trashArticle", 204, ""},
	{"POST", "/api/admin/v1/articles/{articleId}/untrash", "untrashArticle", 204, ""},
	{"GET", "/api/admin/v1/tags", "listTags", 200, "TagList"},
	{"POST", "/api/admin/v1/tags", "createTag", 201, "TagView"},
	{"PATCH", "/api/admin/v1/tags/{tagId}", "renameTag", 200, "TagView"},
	{"POST", "/api/admin/v1/media/upload-policy", "createMediaUploadPolicy", 200, "MediaUploadPolicy"},
	{"POST", "/api/admin/v1/media", "registerMedia", 201, "MediaView"},
	{"GET", "/api/admin/v1/settings/site", "getSiteSettings", 200, "SiteSettingsView"},
	{"PUT", "/api/admin/v1/settings/site", "putSiteSettings", 200, "SiteSettingsView"},
	{"GET", "/api/admin/v1/settings/hotlink", "getHotlinkSettings", 200, "HotlinkSettingsView"},
	{"PUT", "/api/admin/v1/settings/hotlink", "putHotlinkSettings", 200, "HotlinkSettingsView"},
}

func loadAdminContract(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile(adminContractPath)
	require.NoError(t, err)
	require.NoError(t, doc.Validate(context.Background()))
	return doc
}

func TestAdminContractContainsAuthenticationOperations(t *testing.T) {
	doc := loadAdminContract(t)
	require.Equal(t, "loginAdmin", doc.Paths.Find("/api/admin/v1/session").Post.OperationID)
	require.NotNil(t, doc.Paths.Find("/api/admin/v1/session").Delete)
	require.NotNil(t, doc.Paths.Find("/api/admin/v1/me").Get)
}

func TestAdminContractDocumentsPasswordByteLimit(t *testing.T) {
	doc := loadAdminContract(t)

	password := doc.Components.Schemas["LoginRequest"].Value.Properties["password"].Value
	require.Equal(t, "UTF-8 encoded value must be at most 256 bytes.", password.Description)
}

func TestAdminContractContainsExactStage2Operations(t *testing.T) {
	doc := loadAdminContract(t)
	for _, expected := range stage2OperationContracts {
		t.Run(expected.operationID, func(t *testing.T) {
			pathItem := doc.Paths.Find(expected.path)
			require.NotNil(t, pathItem)
			operation := pathItem.Operations()[expected.method]
			require.NotNil(t, operation)
			require.Equal(t, expected.operationID, operation.OperationID)
			response := operation.Responses.Status(expected.success)
			require.NotNil(t, response)
			if expected.schema != "" {
				mediaType := response.Value.Content.Get("application/json")
				require.NotNil(t, mediaType)
				require.Equal(t, "#/components/schemas/"+expected.schema, mediaType.Schema.Ref)
			}
		})
	}
	require.Nil(t, doc.Paths.Find("/img/proxy/{publicKey}"))
}

func TestAdminContractRequestShapesAreExactAndClosed(t *testing.T) {
	doc := loadAdminContract(t)
	expected := map[string][]string{
		"LoginRequest":              {"password", "username"},
		"SaveDraftRequest":          {"contentMd", "coverMediaId", "lockVersion", "summary", "tagIds", "title"},
		"LockVersionRequest":        {"lockVersion"},
		"CreateTagRequest":          {"name"},
		"RenameTagRequest":          {"name"},
		"RegisterMediaRequest":      {"gfsFileId", "originalName"},
		"PutSiteSettingsRequest":    {"aboutMd", "authorBio", "authorName", "filingName", "filingNumber", "homeStatus", "lockVersion", "seoDefaultDescription", "seoDefaultImageMediaId", "seoDefaultTitle", "siteName", "socialLinks"},
		"PutHotlinkSettingsRequest": {"allowEmptyReferer", "entries"},
	}
	for schemaName, wanted := range expected {
		t.Run(schemaName, func(t *testing.T) {
			schemaRef := doc.Components.Schemas[schemaName]
			require.NotNil(t, schemaRef)
			schema := schemaRef.Value
			require.NotNil(t, schema)
			require.NotNil(t, schema.AdditionalProperties.Has)
			require.False(t, *schema.AdditionalProperties.Has)
			actual := make([]string, 0, len(schema.Properties))
			for property := range schema.Properties {
				actual = append(actual, property)
			}
			sort.Strings(actual)
			sort.Strings(wanted)
			require.Equal(t, wanted, actual)
		})
	}

	entry := doc.Components.Schemas["HotlinkEntry"]
	require.NotNil(t, entry)
	require.NotNil(t, entry.Value.AdditionalProperties.Has)
	require.False(t, *entry.Value.AdditionalProperties.Has)
	require.ElementsMatch(t, []string{"hostname", "enabled"}, schemaPropertyNames(entry.Value))
}

func TestAdminContractUsesSignedInt64IdentityAndLockFields(t *testing.T) {
	doc := loadAdminContract(t)
	for name, schemaRef := range doc.Components.Schemas {
		walkSchemaProperties(t, doc, name, schemaRef, map[*openapi3.Schema]bool{})
	}
	for _, pathItem := range doc.Paths.Map() {
		for _, parameter := range pathItem.Parameters {
			assertIdentitySchema(t, parameter.Value.Name, parameter.Value.Schema.Value, 1)
		}
		for _, operation := range pathItem.Operations() {
			for _, parameter := range operation.Parameters {
				if parameter.Value.In == "path" {
					assertIdentitySchema(t, parameter.Value.Name, parameter.Value.Schema.Value, 1)
				}
			}
		}
	}
}

func TestAdminContractEveryErrorUsesProblemResponse(t *testing.T) {
	doc := loadAdminContract(t)
	for path, pathItem := range doc.Paths.Map() {
		for method, operation := range pathItem.Operations() {
			for status, response := range operation.Responses.Map() {
				code, err := strconv.Atoi(status)
				if err != nil || code < 400 {
					continue
				}
				require.Equalf(t, "#/components/responses/ProblemResponse", response.Ref, "%s %s %s", method, path, status)
			}
		}
	}
}

func TestAdminContractDocumentsStage2ProtectionAndDependencies(t *testing.T) {
	doc := loadAdminContract(t)
	for _, expected := range stage2OperationContracts {
		operation := doc.Paths.Find(expected.path).Operations()[expected.method]
		require.NotNilf(t, operation.Responses.Status(401), "%s must document authentication", expected.operationID)
		require.NotNilf(t, operation.Responses.Status(503), "%s must document dependency failure", expected.operationID)
		if expected.method != http.MethodGet {
			require.NotNilf(t, operation.Responses.Status(403), "%s must document Origin rejection", expected.operationID)
		}
	}
}

func walkSchemaProperties(t *testing.T, doc *openapi3.T, owner string, ref *openapi3.SchemaRef, seen map[*openapi3.Schema]bool) {
	t.Helper()
	if ref == nil || ref.Value == nil || seen[ref.Value] {
		return
	}
	seen[ref.Value] = true
	for name, property := range ref.Value.Properties {
		if identityProperty(name) {
			minimum := float64(1)
			if name == "lockVersion" && (owner == "PutSiteSettingsRequest" || owner == "SiteSettingsView") {
				minimum = 0
			}
			assertIdentitySchema(t, owner+"."+name, property.Value, minimum)
		}
		walkSchemaProperties(t, doc, owner+"."+name, property, seen)
	}
	if ref.Value.Items != nil {
		walkSchemaProperties(t, doc, owner+"[]", ref.Value.Items, seen)
	}
}

func assertIdentitySchema(t *testing.T, name string, schema *openapi3.Schema, minimum float64) {
	t.Helper()
	require.NotNil(t, schema, name)
	require.Truef(t, schema.Type.Is("integer"), "%s must be an integer", name)
	require.Equal(t, "int64", schema.Format, name)
	require.NotNil(t, schema.Min, name)
	require.Equal(t, minimum, *schema.Min, name)
}

func identityProperty(name string) bool {
	return name == "id" || name == "lockVersion" ||
		name != "requestId" && name != "appId" && strings.HasSuffix(name, "Id")
}

func schemaPropertyNames(schema *openapi3.Schema) []string {
	properties := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		properties = append(properties, name)
	}
	sort.Strings(properties)
	return properties
}
