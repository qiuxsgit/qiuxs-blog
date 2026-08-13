package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

const (
	adminContractPath       = "../../../contracts/openapi/admin-v1.yaml"
	releaseBundleSchemaPath = "../../../contracts/release-bundle-v1.schema.json"
)

type operationContract struct {
	method      string
	path        string
	operationID string
	success     int
	schema      string
}

var authOperationContracts = []operationContract{
	{"POST", "/api/admin/v1/session", "loginAdmin", 200, "AdminView"},
	{"DELETE", "/api/admin/v1/session", "logoutAdmin", 204, ""},
	{"GET", "/api/admin/v1/me", "getCurrentAdmin", 200, "AdminView"},
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

var releaseAdminOperationContracts = []operationContract{
	{"GET", "/api/admin/v1/builder", "getBuilderConfig", 200, "BuilderConfigView"},
	{"PUT", "/api/admin/v1/builder", "putBuilderConfig", 200, "BuilderConfigView"},
	{"POST", "/api/admin/v1/builder/test", "testBuilderConfig", 204, ""},
	{"POST", "/api/admin/v1/releases", "createRelease", 202, "CreateReleaseResult"},
	{"GET", "/api/admin/v1/releases", "listReleases", 200, "ReleaseList"},
	{"GET", "/api/admin/v1/releases/{releaseId}", "getRelease", 200, "ReleaseView"},
	{"POST", "/api/admin/v1/releases/{releaseId}/retry", "retryRelease", 202, "PublishJobView"},
}

var releaseInternalOperationContracts = []operationContract{
	{"GET", "/api/internal/v1/releases/{releaseId}/bundle", "getReleaseBundle", 200, "ReleaseBundle"},
	{"POST", "/api/internal/v1/jenkins/callback", "acceptJenkinsCallback", 204, ""},
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

func TestAdminContractContainsExactReleaseAndJenkinsOperations(t *testing.T) {
	doc := loadAdminContract(t)
	for _, expected := range append(append([]operationContract(nil), releaseAdminOperationContracts...), releaseInternalOperationContracts...) {
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
}

func TestReleaseBundleSchemaRequiresExactImmutablePublicShape(t *testing.T) {
	schema := loadReleaseBundleSchema(t)
	valid := map[string]any{
		"schemaVersion": 1,
		"releaseId":     int64(7),
		"generatedAt":   "2026-08-13T12:00:00Z",
		"site": map[string]any{
			"name": "", "authorBio": "", "aboutMarkdown": "", "filingName": "长安休息室", "filingNumber": "浙ICP备17057726号-1",
			"socialLinks": []any{map[string]any{"label": "GitHub", "url": "https://github.com/qiuxsgit"}},
		},
		"tags": []any{map[string]any{"id": int64(1), "name": "Go", "slug": "go"}},
		"articles": []any{map[string]any{
			"articleId": int64(2), "revisionId": int64(3), "slug": "example", "title": "", "summary": "",
			"contentMarkdown": "", "contentHash": "sha256:" + strings.Repeat("b", 64),
			"publishedAt": "2026-08-13T12:00:00Z", "tags": []any{"go"},
		}},
		"checksum": "sha256:" + strings.Repeat("a", 64),
	}
	require.NoError(t, schema.Validate(valid))

	invalidCases := map[string]func(map[string]any){
		"unknown root field":          func(value map[string]any) { value["builderToken"] = "secret" },
		"unsigned overflow":           func(value map[string]any) { value["releaseId"] = json.Number("9223372036854775808") },
		"nonpositive release ID":      func(value map[string]any) { value["releaseId"] = int64(0) },
		"fractional release ID":       func(value map[string]any) { value["releaseId"] = json.Number("1.5") },
		"wrong version":               func(value map[string]any) { value["schemaVersion"] = 2 },
		"missing required field":      func(value map[string]any) { delete(value, "generatedAt") },
		"invalid generated timestamp": func(value map[string]any) { value["generatedAt"] = "2026-08-13" },
		"uppercase checksum":          func(value map[string]any) { value["checksum"] = "sha256:" + strings.Repeat("A", 64) },
		"extra site field":            func(value map[string]any) { value["site"].(map[string]any)["homeStatus"] = "private" },
		"extra social field": func(value map[string]any) {
			value["site"].(map[string]any)["socialLinks"].([]any)[0].(map[string]any)["token"] = "secret"
		},
		"extra tag field":     func(value map[string]any) { value["tags"].([]any)[0].(map[string]any)["position"] = 0 },
		"extra article field": func(value map[string]any) { value["articles"].([]any)[0].(map[string]any)["draft"] = true },
		"invalid content hash": func(value map[string]any) {
			value["articles"].([]any)[0].(map[string]any)["contentHash"] = strings.Repeat("b", 64)
		},
		"invalid published timestamp": func(value map[string]any) {
			value["articles"].([]any)[0].(map[string]any)["publishedAt"] = "not-a-time"
		},
	}
	for name, mutate := range invalidCases {
		t.Run(name, func(t *testing.T) {
			copy := cloneJSONValue(t, valid)
			mutate(copy)
			require.Error(t, schema.Validate(copy))
		})
	}
}

func TestAdminContractPreviewContainsImmutableSlugAndDraft(t *testing.T) {
	doc := loadAdminContract(t)
	preview := doc.Components.Schemas["PreviewView"]
	require.NotNil(t, preview)
	require.NotNil(t, preview.Value)
	require.ElementsMatch(t, []string{"slug", "draft"}, preview.Value.Required)
	require.ElementsMatch(t, []string{"slug", "draft"}, schemaPropertyNames(preview.Value))
	require.Equal(t, "^[a-z0-9_-]{12}$", preview.Value.Properties["slug"].Value.Pattern)
	require.Equal(t, "#/components/schemas/DraftView", preview.Value.Properties["draft"].Ref)
}

func TestAdminContractContainsExactOperationSet(t *testing.T) {
	doc := loadAdminContract(t)
	wanted := make([]string, 0, len(authOperationContracts)+len(stage2OperationContracts)+len(releaseAdminOperationContracts)+len(releaseInternalOperationContracts))
	operations := append(append([]operationContract(nil), authOperationContracts...), stage2OperationContracts...)
	operations = append(operations, releaseAdminOperationContracts...)
	operations = append(operations, releaseInternalOperationContracts...)
	for _, operation := range operations {
		wanted = append(wanted, operation.method+" "+operation.path+" "+operation.operationID)
	}
	actual := make([]string, 0, len(wanted))
	for path, pathItem := range doc.Paths.Map() {
		for method, operation := range pathItem.Operations() {
			actual = append(actual, method+" "+path+" "+operation.OperationID)
		}
	}
	sort.Strings(wanted)
	sort.Strings(actual)
	require.Equal(t, wanted, actual)
}

func TestAdminContractDefinesAndAppliesExactSessionSecurity(t *testing.T) {
	doc := loadAdminContract(t)
	schemeRef := doc.Components.SecuritySchemes["AdminSession"]
	require.NotNil(t, schemeRef)
	require.NotNil(t, schemeRef.Value)
	require.Equal(t, "apiKey", schemeRef.Value.Type)
	require.Equal(t, "cookie", schemeRef.Value.In)
	require.Equal(t, "qx_blog_session", schemeRef.Value.Name)
	require.Empty(t, schemeRef.Value.Scheme)
	require.Empty(t, schemeRef.Value.BearerFormat)
	require.Nil(t, doc.Paths.Find("/api/admin/v1/session").Post.Security, "login must remain unauthenticated")
	logoutSecurity := doc.Paths.Find("/api/admin/v1/session").Delete.Security
	require.NotNil(t, logoutSecurity, "logout must explicitly preserve anonymous idempotency")
	require.Empty(t, *logoutSecurity, "logout must remain unauthenticated")

	protected := append(append([]operationContract(nil), stage2OperationContracts...), releaseAdminOperationContracts...)
	protected = append(protected,
		operationContract{"GET", "/api/admin/v1/me", "getCurrentAdmin", 200, "AdminView"},
	)
	for _, expected := range protected {
		pathItem := doc.Paths.Find(expected.path)
		require.NotNilf(t, pathItem, "%s path", expected.operationID)
		operation := pathItem.Operations()[expected.method]
		require.NotNilf(t, operation.Security, "%s security", expected.operationID)
		require.Equalf(t, openapi3.SecurityRequirements{{"AdminSession": []string{}}}, *operation.Security, "%s security", expected.operationID)
	}

	bundleSecurity := doc.Components.SecuritySchemes["BundleToken"]
	require.NotNil(t, bundleSecurity)
	require.Equal(t, "http", bundleSecurity.Value.Type)
	require.Equal(t, "bearer", bundleSecurity.Value.Scheme)
	callbackSecurity := doc.Components.SecuritySchemes["JenkinsSignature"]
	require.NotNil(t, callbackSecurity)
	require.Equal(t, "apiKey", callbackSecurity.Value.Type)
	require.Equal(t, "header", callbackSecurity.Value.In)
	require.Equal(t, "X-Jenkins-Signature", callbackSecurity.Value.Name)
	bundlePath := doc.Paths.Find("/api/internal/v1/releases/{releaseId}/bundle")
	require.NotNil(t, bundlePath)
	require.Equal(t, openapi3.SecurityRequirements{{"BundleToken": []string{}}}, *bundlePath.Get.Security)
	callbackPath := doc.Paths.Find("/api/internal/v1/jenkins/callback")
	require.NotNil(t, callbackPath)
	require.Equal(t, openapi3.SecurityRequirements{{"JenkinsSignature": []string{}}}, *callbackPath.Post.Security)
}

func TestAdminContractDocumentsExactOperationResponseStatuses(t *testing.T) {
	doc := loadAdminContract(t)
	expected := map[string][]string{
		"loginAdmin":              {"200", "400", "401", "403", "429", "503"},
		"logoutAdmin":             {"204", "403", "503"},
		"getCurrentAdmin":         {"200", "401", "503"},
		"listArticles":            {"200", "400", "401", "503"},
		"createArticle":           {"201", "400", "401", "403", "409", "503"},
		"getArticle":              {"200", "400", "401", "404", "503"},
		"saveArticleDraft":        {"200", "400", "401", "403", "404", "409", "422", "503"},
		"getArticlePreview":       {"200", "400", "401", "404", "503"},
		"listArticleVersions":     {"200", "400", "401", "404", "503"},
		"createArticleVersion":    {"201", "400", "401", "403", "404", "409", "422", "503"},
		"restoreArticleVersion":   {"200", "400", "401", "403", "404", "409", "422", "503"},
		"trashArticle":            {"204", "400", "401", "403", "404", "409", "503"},
		"untrashArticle":          {"204", "400", "401", "403", "404", "409", "503"},
		"listTags":                {"200", "400", "401", "503"},
		"createTag":               {"201", "400", "401", "403", "409", "503"},
		"renameTag":               {"200", "400", "401", "403", "404", "409", "503"},
		"createMediaUploadPolicy": {"200", "400", "401", "403", "503"},
		"registerMedia":           {"201", "400", "401", "403", "409", "422", "503"},
		"getSiteSettings":         {"200", "400", "401", "503"},
		"putSiteSettings":         {"200", "400", "401", "403", "409", "422", "503"},
		"getHotlinkSettings":      {"200", "400", "401", "503"},
		"putHotlinkSettings":      {"200", "400", "401", "403", "409", "422", "503"},
		"getBuilderConfig":        {"200", "400", "401", "404", "503"},
		"putBuilderConfig":        {"200", "400", "401", "403", "409", "422", "503"},
		"testBuilderConfig":       {"204", "400", "401", "403", "412", "503"},
		"createRelease":           {"202", "400", "401", "403", "409", "412", "503"},
		"listReleases":            {"200", "400", "401", "503"},
		"getRelease":              {"200", "400", "401", "404", "503"},
		"retryRelease":            {"202", "400", "401", "403", "404", "409", "412", "503"},
		"getReleaseBundle":        {"200", "400", "401", "404", "409", "503"},
		"acceptJenkinsCallback":   {"204", "400", "401", "409", "503"},
	}
	for path, pathItem := range doc.Paths.Map() {
		for _, operation := range pathItem.Operations() {
			statuses := make([]string, 0, operation.Responses.Len())
			for status := range operation.Responses.Map() {
				statuses = append(statuses, status)
			}
			sort.Strings(statuses)
			wanted, ok := expected[operation.OperationID]
			require.Truef(t, ok, "unexpected operation %s at %s", operation.OperationID, path)
			sort.Strings(wanted)
			require.Equal(t, wanted, statuses, operation.OperationID)
		}
	}
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
		"PutBuilderConfigRequest":   {"baseUrl", "enabled", "jobName", "name", "token", "username"},
		"CreateReleaseRequest":      {"articleId", "mode"},
		"JenkinsCallbackRequest":    {"buildNumber", "errorSummary", "nonce", "releaseId", "stage", "status", "timestamp"},
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

	builderView := doc.Components.Schemas["BuilderConfigView"]
	require.NotNil(t, builderView)
	require.ElementsMatch(t, []string{"id", "name", "baseUrl", "username", "jobName", "enabled", "tokenConfigured"}, schemaPropertyNames(builderView.Value))
	require.NotContains(t, schemaPropertyNames(builderView.Value), "token")
	require.NotContains(t, schemaPropertyNames(builderView.Value), "tokenCiphertext")

	createResult := doc.Components.Schemas["CreateReleaseResult"]
	require.NotNil(t, createResult)
	require.NotNil(t, createResult.Value.AdditionalProperties.Has)
	require.False(t, *createResult.Value.AdditionalProperties.Has)
	require.ElementsMatch(t, []string{"release", "job"}, createResult.Value.Required)
	require.Equal(t, "#/components/schemas/ReleaseView", createResult.Value.Properties["release"].Ref)
	require.Equal(t, "#/components/schemas/PublishJobView", createResult.Value.Properties["job"].Ref)
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

func TestAdminContractDocumentsReleaseProtectionAndDependencies(t *testing.T) {
	doc := loadAdminContract(t)
	for _, expected := range releaseAdminOperationContracts {
		pathItem := doc.Paths.Find(expected.path)
		require.NotNilf(t, pathItem, "%s path", expected.operationID)
		operation := pathItem.Operations()[expected.method]
		require.NotNilf(t, operation.Responses.Status(401), "%s must document authentication", expected.operationID)
		require.NotNilf(t, operation.Responses.Status(503), "%s must document dependency failure", expected.operationID)
		if expected.method != http.MethodGet {
			require.NotNilf(t, operation.Responses.Status(403), "%s must document Origin rejection", expected.operationID)
		}
	}
	for _, expected := range releaseInternalOperationContracts {
		pathItem := doc.Paths.Find(expected.path)
		require.NotNilf(t, pathItem, "%s path", expected.operationID)
		operation := pathItem.Operations()[expected.method]
		require.NotNilf(t, operation.Responses.Status(401), "%s must document independent authentication", expected.operationID)
		require.NotNilf(t, operation.Responses.Status(503), "%s must document dependency failure", expected.operationID)
	}
}

func loadReleaseBundleSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	file, err := os.Open(releaseBundleSchemaPath)
	require.NoError(t, err)
	defer file.Close()
	document, err := jsonschema.UnmarshalJSON(file)
	require.NoError(t, err)
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	require.NoError(t, compiler.AddResource("release-bundle-v1.schema.json", document))
	schema, err := compiler.Compile("release-bundle-v1.schema.json")
	require.NoError(t, err)
	return schema
}

func cloneJSONValue(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	var copy map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&copy))
	return copy
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
