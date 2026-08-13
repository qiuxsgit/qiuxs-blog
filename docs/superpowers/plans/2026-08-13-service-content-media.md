# Service Content, Revisions, Settings, and Media Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the authenticated Go service with article drafts and immutable versions, stable tag snapshots, optimistic autosave, preview and trash lifecycle operations, publishable site settings and filing validation, GFS-backed media registration, and an immediately invalidated Referer-aware public image redirect.

**Architecture:** Keep the generated Admin API as a full-path OpenAPI contract registered on the existing zero-prefix authenticated Gin group. Add article, revision, tag, media, and settings domain services behind small interfaces; MySQL adapters own SQL and receive the one process-wide Redis-backed `*idgen.Generator`. Browser uploads go directly to GFS with a short policy signed locally by this service, while media registration verifies GFS metadata through an injected HTTP client. Public `/img/proxy/{publicKey}` remains outside Admin middleware, reads an in-memory hotlink policy cache backed by MySQL, and locally signs a short GFS read URL before returning `302` without proxying bytes.

**Tech Stack:** Go `1.25.7`, Gin `v1.12.0`, `database/sql` with MySQL driver `v1.10.0`, go-redis `v9.22.0`, oapi-codegen `v2.8.0`, goldmark `v1.8.5`, Testify `v1.11.1`, sqlmock `v1.5.2`, miniredis `v2.38.0`, standard-library `httptest` and fakes.

## Global Constraints

- Run every Service Go command with `GOTOOLCHAIN=go1.25.7`; do not accept another locally selected toolchain.
- Write schema changes only by appending reviewed DDL to `service/sqls/develop/develop.sql`. Do not create release SQL, a migration command, a migration library, or startup DDL/data initialization. Operators execute development SQL manually.
- Every persistent table has `id BIGINT NOT NULL PRIMARY KEY`; never use `UNSIGNED` or `AUTO_INCREMENT`. Every Go and OpenAPI entity ID is `int64`/`format: int64`.
- Construct exactly one `idgen.RedisCounter` and one `*idgen.Generator` in `app.Build`; inject that generator into every Stage 2 repository. A Redis allocation failure rejects the insert; there is no alternate ID source.
- Redis IDs are identity only. Lists sort by an explicit timestamp or revision sequence and use ID only as a deterministic tie-breaker.
- Keep controllers free of SQL and cross-domain table mutation. Domain services orchestrate; MySQL adapters own transactions and SQL.
- Use strict TDD for every task: add the focused test, run it and record the expected assertion-level failure, add the minimum implementation, then rerun to green before the task commit.
- Automated tests use only sqlmock, miniredis, in-memory fakes, and `httptest`. They must not start Docker/Testcontainers or contact deployed MySQL, Redis, GFS, or OSS.
- Keep Admin unsafe requests behind exact Origin checking and Redis Session authentication. Keep `/img/proxy/{publicKey}` public and protected only by the documented weak Referer policy.
- Never log request bodies, cookies, authorization values, Markdown bodies, GFS app/public-read secrets, policies, nonces, signatures, signed URLs, MySQL DSNs, or Redis passwords.
- Return the existing sanitized `application/problem+json` shape with the request ID. New stable codes are `revision_conflict`, `settings_conflict`, `article_state_conflict`, `article_must_be_unpublished`, `tag_conflict`, `invalid_content`, `invalid_media`, `hotlink_forbidden`, and `dependency_unavailable`.
- Frozen revisions are immutable. Autosave updates only the current `editing` row with a matching `lock_version`. Version creation and restore freeze the current draft and insert a new editing row.
- Autosave may retain a transient `blob:` URL so local editor recovery is not falsely acknowledged as a durable image. `revision.ValidateFreezable` rejects any `blob:` URL before manual version creation; Stage 3 must call the same validator before a publish snapshot.
- Site settings are a publishable draft, while hotlink settings are immediate runtime configuration. `settings.ValidatePublishable` is the Stage 3 filing gate and must reject empty filing name or number even if invalid rows predate current validation.
- GFS upload policy is fixed at 60 seconds and fixed to `blog/{{year}}/{{month}}/{{uuid}}.{{fileExt}}`; neither request JSON nor a query parameter can provide `savePath`.
- Accept only JPEG, PNG, WebP, and GIF media, at most 10 MiB, with positive dimensions no greater than 12000 by 12000. Reject SVG and MIME/extension disagreements.
- Public media responses always set `Cache-Control: no-store`, including `302`, `403`, `404`, and dependency failures.
- The deployed GFS revision must contain `f9b8256` (actual object metadata endpoint) and `bcf8725` (temporary redirect). This repository does not modify `/Users/qiuxs/codes/qiuxs/go-file-server` in this stage.
- Release snapshots, publishing/unpublishing orchestration, recent publish results, Jenkins, Bundle generation, and the Astro renderer remain Stage 3 or later. Stage 2 only provides reusable freeze and filing validation boundaries.

---

## Planned File Map

```text
contracts/openapi/admin-v1.yaml                         Add complete Stage 2 Admin paths and schemas
docs/contracts/gfs-blog-media.md                       Pin multipart, metadata, and read-signing compatibility
service/go.mod                                          Add exact goldmark dependency
service/go.sum                                          Record resolved goldmark checksums
service/README.md                                       Document Stage 2 env, SQL, GFS, and HTTP behavior
service/internal/app/app.go                             Compose the shared generator and all Stage 2 services/routes
service/internal/app/app_test.go                        Dependency validation and route/middleware composition
service/internal/app/docs_test.go                       Stage 2 operator-documentation contract
service/internal/config/config.go                       Parse and validate GFS configuration
service/internal/config/config_test.go                  Direct and environment-loaded GFS config coverage
service/internal/dbtable/names.go                       Audited table-name constants for idgen and healing
service/internal/dbtable/names_test.go                  Ensure every Stage 2 table is an accepted idgen table
service/internal/randomkey/generator.go                 Concurrency-safe slug/public-key/nonce generation
service/internal/randomkey/generator_test.go            Alphabet, entropy-length, and nil/failure tests
service/internal/tag/model.go                           Tag values and domain errors
service/internal/tag/repository.go                      Tag persistence boundary
service/internal/tag/repository_mysql.go                MySQL tag adapter using shared IDs
service/internal/tag/repository_mysql_test.go           sqlmock tag persistence tests
service/internal/tag/service.go                         Normalize, create, list, rename, and snapshot tags
service/internal/tag/service_test.go                    Tag behavior and stable-slug tests
service/internal/media/model.go                         Media, upload policy, metadata, and domain errors
service/internal/media/gfs_signer.go                    Exact local GFS upload/read signatures
service/internal/media/gfs_signer_test.go               Fixed-vector signature tests
service/internal/media/gfs_client.go                    Actual GFS metadata HTTP client
service/internal/media/gfs_client_test.go               httptest metadata envelope and sanitization tests
service/internal/media/repository.go                    Media persistence boundary
service/internal/media/repository_mysql.go              MySQL media adapter using shared IDs
service/internal/media/repository_mysql_test.go         sqlmock insert/find tests
service/internal/media/service.go                       Policy issuance, metadata validation, and registration
service/internal/media/service_test.go                  Fake-based media policy/registration tests
service/internal/media/proxy.go                         Authorization-first public redirect resolution
service/internal/media/proxy_test.go                    Proxy ordering, error, and local-signing tests
service/internal/article/model.go                       Article identity, state, summaries, and errors
service/internal/article/repository.go                  Article persistence boundary
service/internal/article/repository_mysql.go            MySQL article identity/list/trash adapter
service/internal/article/repository_mysql_test.go       sqlmock article transaction and lifecycle tests
service/internal/article/service.go                     Slug retry and lifecycle orchestration
service/internal/article/service_test.go                Create/list/trash/untrash behavior
service/internal/revision/model.go                      Draft/version/snapshot value types and errors
service/internal/revision/markdown.go                   Markdown policy and stable media-key extraction
service/internal/revision/markdown_test.go              GFM/raw-HTML/blob/media-reference cases
service/internal/revision/hash.go                       Canonical revision content hash
service/internal/revision/hash_test.go                  Deterministic hash vectors
service/internal/revision/repository.go                 Draft/version persistence boundary
service/internal/revision/repository_mysql.go           Optimistic save/version/restore MySQL transactions
service/internal/revision/repository_mysql_test.go      sqlmock conflict, copy, rollback, and immutability tests
service/internal/revision/service.go                    Resolve snapshots and orchestrate draft/version/preview
service/internal/revision/service_test.go               Fake-based save/version/restore/preview rules
service/internal/settings/model.go                      Site and hotlink values, defaults, and validation errors
service/internal/settings/repository.go                 Site/hotlink persistence boundaries
service/internal/settings/repository_mysql.go           MySQL singleton and allowlist adapters using shared IDs
service/internal/settings/repository_mysql_test.go      sqlmock defaults/save/replace tests
service/internal/settings/site_service.go               Publishable site-setting validation and persistence
service/internal/settings/site_service_test.go          Defaults, optimistic conflict, and filing-gate tests
service/internal/settings/hotlink.go                    Host normalization, policy service, and cache
service/internal/settings/hotlink_test.go               Exact-match/default/cache-invalidation tests
service/internal/httpapi/admin_handler.go               Composite generated ServerInterface implementation
service/internal/httpapi/article_handler.go             Article/draft/version/preview HTTP translation
service/internal/httpapi/tag_handler.go                 Tag HTTP translation
service/internal/httpapi/media_handler.go               Upload-policy and registration HTTP translation
service/internal/httpapi/settings_handler.go            Site/hotlink HTTP translation
service/internal/httpapi/json.go                        Strict bounded JSON request decoding
service/internal/httpapi/log_context.go                 Safe admin/article access-log attributes
service/internal/httpapi/media_proxy_handler.go         Public Referer check and GFS redirect handler
service/internal/httpapi/stage2_handler_test.go          Auth, Origin, decoding, mapping, and response tests
service/internal/httpapi/media_proxy_handler_test.go    Public no-store/Referer/redirect tests
service/internal/httpapi/problem.go                     Map Stage 2 errors to sanitized Problems
service/internal/httpapi/contract_test.go               Validate all full paths, IDs, and generated contract
service/internal/httpapi/admin.gen.go                    Regenerated OpenAPI models/routes; never hand-edit
service/sqls/develop/develop.sql                         Append Stage 2 DDL only
service/sqls/sql_contract_test.go                        Signed-ID, constraint, index, and manual-SQL checks
service/tests/flow/content_media_test.go                 Real router Stage 2 flow with sqlmock/miniredis/fake GFS
README.md                                                Report implemented Service Stage 2 and link its guide
```

Unit tests remain beside their production files. Generated `admin.gen.go` is reviewed as output but changed only through `make generate`.

## Fixed Domain and HTTP Contracts

Use these declarations as the type source of truth while implementing. Do not substitute `uint64`, `int`, string IDs, timestamp-derived IDs, or nullable zero sentinels.

```go
// service/internal/article/model.go
type State string
const (
	StateActive  State = "active"
	StateTrashed State = "trashed"
)
type Article struct {
	ID                  int64
	Slug                string
	DraftRevisionID     int64
	PublishedRevisionID *int64
	State               State
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
type Summary struct {
	Article
	DraftTitle     string
	DraftUpdatedAt time.Time
}
var (
	ErrNotFound            = errors.New("article not found")
	ErrSlugConflict        = errors.New("article slug conflict")
	ErrMustBeUnpublished   = errors.New("article must be unpublished before trash")
	ErrStateConflict       = errors.New("article state conflict")
)

// service/internal/tag/model.go
type Tag struct { ID int64; Name, Slug string; CreatedAt, UpdatedAt time.Time }
type Snapshot struct { TagID int64; Name, Slug string; Position int }
var (
	ErrNotFound     = errors.New("tag not found")
	ErrNameConflict = errors.New("tag name conflict")
	ErrSlugConflict = errors.New("tag slug conflict")
)

// service/internal/media/model.go
type Media struct {
	ID int64
	PublicKey string
	GFSFileID int64
	OriginalName, MIMEType string
	FileSize int64
	Width, Height int
	State string
	CreatedAt, UpdatedAt time.Time
}
type Metadata struct { FileID int64; FileName, ContentType string; FileSize int64; Width, Height int }
type UploadPolicy struct { UploadURL, AppID, Policy, Signature, Timestamp, Expire, Nonce, FileField string }
type Reference struct { MediaID int64; PublicKey, Purpose string; Position int }
var (
	ErrNotFound             = errors.New("media not found")
	ErrInvalidMetadata      = errors.New("invalid media metadata")
	ErrPublicKeyConflict    = errors.New("media public key conflict")
	ErrDependencyUnavailable = errors.New("media dependency unavailable")
)

// service/internal/revision/model.go
type Status string
type Reason string
const (
	StatusEditing Status = "editing"
	StatusFrozen  Status = "frozen"
	ReasonDraft Reason = "draft"
	ReasonManualVersion Reason = "manual_version"
	ReasonPublishSnapshot Reason = "publish_snapshot"
)
type Content struct {
	Title, Summary string
	CoverMediaID *int64
	ContentMD string
	TagIDs []int64
}
type Draft struct {
	ID, ArticleID, RevisionNo, LockVersion int64
	Status Status
	Reason Reason
	Title, Summary string
	CoverMediaID *int64
	ContentMD, ContentHash string
	Tags []tag.Snapshot
	Media []media.Reference
	CreatedAt, UpdatedAt time.Time
}
type Version struct { Draft }
var (
	ErrNotFound       = errors.New("revision not found")
	ErrConflict       = errors.New("revision optimistic lock conflict")
	ErrInvalidContent = errors.New("revision content is invalid")
	ErrNotFrozen      = errors.New("revision is not frozen")
	ErrArticleInactive = errors.New("article is not active")
)

// service/internal/settings/model.go
const FilingURL = "https://beian.miit.gov.cn/"
type SocialLink struct { Label, URL string }
type Site struct {
	ID, LockVersion int64
	SiteName, AuthorName, AuthorBio, HomeStatus, AboutMD string
	SocialLinks []SocialLink
	SEODefaultTitle, SEODefaultDescription string
	SEODefaultImageMediaID *int64
	FilingName, FilingNumber string
	UpdatedAt time.Time
}
type HotlinkEntry struct { ID int64; Hostname string; Enabled bool }
type HotlinkPolicy struct { AllowEmptyReferer bool; Entries []HotlinkEntry }
var (
	ErrNotFound = errors.New("settings not found")
	ErrConflict = errors.New("settings optimistic lock conflict")
	ErrInvalid  = errors.New("settings are invalid")
)
```

The persistence and service seams are fixed as follows:

```go
// service/internal/tag/repository.go
type Repository interface {
	Create(context.Context, string, string, time.Time) (Tag, error)
	List(context.Context) ([]Tag, error)
	FindByIDs(context.Context, []int64) ([]Tag, error)
	Rename(context.Context, int64, string, time.Time) (Tag, error)
}

// service/internal/media/repository.go
type Repository interface {
	Create(context.Context, NewMedia, time.Time) (Media, error)
	FindByGFSFileID(context.Context, int64) (Media, error)
	FindActiveByID(context.Context, int64) (Media, error)
	FindActiveByIDs(context.Context, []int64) ([]Media, error)
	FindActiveByPublicKeys(context.Context, []string) ([]Media, error)
	FindActiveByPublicKey(context.Context, string) (Media, error)
}
type MetadataReader interface { Metadata(context.Context, int64) (Metadata, error) }
type ReadURLSigner interface { ReadURL(Media, time.Time) (string, error) }

// service/internal/article/repository.go
type Repository interface {
	Create(context.Context, string, time.Time) (Article, error)
	FindByID(context.Context, int64) (Article, error)
	List(context.Context, State) ([]Summary, error)
	SetState(context.Context, int64, State, State, time.Time) error
}
type DraftReader interface { GetDraft(context.Context, int64) (revision.Draft, error) }

// service/internal/revision/repository.go
type Repository interface {
	GetDraft(context.Context, int64) (Draft, error)
	SaveDraft(context.Context, int64, int64, PreparedContent, time.Time) (Draft, error)
	CreateVersion(context.Context, int64, int64, time.Time) (Version, Draft, error)
	ListVersions(context.Context, int64) ([]Version, error)
	RestoreVersion(context.Context, int64, int64, int64, time.Time) (Draft, error)
}
type TagResolver interface { Snapshots(context.Context, []int64) ([]tag.Snapshot, error) }
type MediaResolver interface { ResolveReferences(context.Context, *int64, []string) (*media.Media, []media.Reference, error) }

// service/internal/settings/repository.go
type SiteRepository interface {
	GetSite(context.Context) (Site, error)
	CreateSite(context.Context, Site, time.Time) (Site, error)
	UpdateSite(context.Context, Site, int64, time.Time) (Site, error)
}
type HotlinkRepository interface {
	GetHotlinkPolicy(context.Context) (HotlinkPolicy, error)
	ReplaceHotlinkPolicy(context.Context, HotlinkPolicy, time.Time) (HotlinkPolicy, error)
}
type HotlinkPolicyProvider interface { Current(context.Context) (HotlinkPolicy, error) }
```

`PreparedContent` contains validated scalar content, ordered tag snapshots, the resolved optional cover, ordered unique body media references, and the canonical SHA-256 hash. Repository methods never accept unresolved client IDs or public keys.

The Admin contract contains these exact routes:

| Method | Path | Operation ID | Success |
| --- | --- | --- | --- |
| GET | `/api/admin/v1/articles` | `listArticles` | `200 ArticleList` |
| POST | `/api/admin/v1/articles` | `createArticle` | `201 ArticleDetail` |
| GET | `/api/admin/v1/articles/{articleId}` | `getArticle` | `200 ArticleDetail` |
| PUT | `/api/admin/v1/articles/{articleId}/draft` | `saveArticleDraft` | `200 DraftView` |
| GET | `/api/admin/v1/articles/{articleId}/preview` | `getArticlePreview` | `200 PreviewView` |
| GET | `/api/admin/v1/articles/{articleId}/versions` | `listArticleVersions` | `200 RevisionList` |
| POST | `/api/admin/v1/articles/{articleId}/versions` | `createArticleVersion` | `201 VersionResult` |
| POST | `/api/admin/v1/articles/{articleId}/versions/{revisionId}/restore` | `restoreArticleVersion` | `200 DraftView` |
| POST | `/api/admin/v1/articles/{articleId}/trash` | `trashArticle` | `204` |
| POST | `/api/admin/v1/articles/{articleId}/untrash` | `untrashArticle` | `204` |
| GET | `/api/admin/v1/tags` | `listTags` | `200 TagList` |
| POST | `/api/admin/v1/tags` | `createTag` | `201 TagView` |
| PATCH | `/api/admin/v1/tags/{tagId}` | `renameTag` | `200 TagView` |
| POST | `/api/admin/v1/media/upload-policy` | `createMediaUploadPolicy` | `200 MediaUploadPolicy` |
| POST | `/api/admin/v1/media` | `registerMedia` | `201 MediaView` |
| GET | `/api/admin/v1/settings/site` | `getSiteSettings` | `200 SiteSettingsView` |
| PUT | `/api/admin/v1/settings/site` | `putSiteSettings` | `200 SiteSettingsView` |
| GET | `/api/admin/v1/settings/hotlink` | `getHotlinkSettings` | `200 HotlinkSettingsView` |
| PUT | `/api/admin/v1/settings/hotlink` | `putHotlinkSettings` | `200 HotlinkSettingsView` |

`POST /articles` has no request body and creates an empty draft. `PUT /draft` accepts `{lockVersion,title,summary,coverMediaId,contentMd,tagIds}`. Version creation and restore accept `{lockVersion}`. Article list accepts optional `state=active|trashed`, defaulting to `active`. The public `GET /img/proxy/{publicKey}` is deliberately hand-registered outside `admin-v1.yaml` so generated Admin routes cannot accidentally put it behind Session and Origin middleware.

---

### Task 1: Append the Signed-BIGINT Stage 2 Schema and Table Constants

**Files:**
- Modify: `service/sqls/develop/develop.sql`
- Modify: `service/sqls/sql_contract_test.go`
- Create: `service/internal/dbtable/names.go`
- Create: `service/internal/dbtable/names_test.go`

**Interfaces:**
- Consumes: existing `idgen.Generator` table-name validation and manual development SQL lifecycle.
- Produces: constants `Articles`, `ArticleRevisions`, `Tags`, `ArticleRevisionTags`, `Media`, `ArticleRevisionMedia`, `SiteSettings`, `HotlinkSettings`, and `RefererAllowlist` used by every Stage 2 insert/heal path.

- [ ] **Step 1: Add failing SQL and table-name contract tests**

Add table-driven assertions that all nine Stage 2 tables exist, each contains `id BIGINT NOT NULL` and `PRIMARY KEY (id)`, the entire file contains neither `AUTO_INCREMENT` nor `UNSIGNED`, and no migration runner or release SQL was created. Assert these named constraints/indexes: `uk_articles_slug`, `uk_article_revisions_no`, `uk_article_revisions_editing`, `uk_tags_name`, `uk_tags_slug`, `uk_media_public_key`, `uk_media_gfs_file_id`, `uk_site_settings_singleton`, `uk_hotlink_settings_singleton`, and `uk_referer_allowlist_hostname`. Add a `dbtable.All` test that passes every value through `generator, err := idgen.New(fakeCounter, nil, 1, 1, false)` followed by `generator.Next(ctx, table)` and rejects accidental omissions.

- [ ] **Step 2: Run the focused tests and record the RED for the schema**

Run:

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./sqls ./internal/dbtable -v
```

Expected: FAIL because the Stage 2 tables and `dbtable` package do not exist. A compiler/dependency failure is not the accepted RED; make the test compile while retaining the missing-schema assertion failure.

- [ ] **Step 3: Add constants and append the complete development DDL**

Create constant strings matching the table names exactly. Append DDL in dependency order: `media`, `tags`, `articles`, `article_revisions`, the two revision association tables, then `site_settings`, `hotlink_settings`, and `referer_allowlist`; finally add the nullable article revision-pointer foreign keys. Use `CHECK` constraints for article state, revision status/reason, media state, association purpose, positive size/dimensions, positive lock versions, and singleton keys. Use `UNIQUE(article_id, revision_no)` plus a generated nullable column `editing_article_id BIGINT GENERATED ALWAYS AS (CASE WHEN status = 'editing' THEN article_id ELSE NULL END) STORED` and `UNIQUE(editing_article_id)` to enforce one editing revision per article. Use `LONGTEXT` for Markdown, `CHAR(64)` for hashes, `JSON` for social links, `DATETIME(6)` audit columns, and `ON DELETE RESTRICT` foreign keys. Do not insert seed rows.

Give both revision association tables a non-negative `position` column plus unique `(revision_id, position)` constraints; media positions place the optional cover first and then body references in first-use order. The `site_settings` columns are the fields in `settings.Site`; `hotlink_settings` contains `singleton_key` and `allow_empty_referer`; `referer_allowlist` contains `hostname` and `enabled`. The virtual default rows are application behavior added in Tasks 8 and 9, so Redis still allocates every persisted setting ID.

- [ ] **Step 4: Run schema tests and inspect the DDL manually**

Run:

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./sqls ./internal/dbtable -v
rg -n 'AUTO_INCREMENT|UNSIGNED|CREATE TABLE|PRIMARY KEY|UNIQUE KEY|FOREIGN KEY' sqls/develop/develop.sql
```

Expected: PASS; the search shows nine new tables and their constraints, and no forbidden integer modifiers.

- [ ] **Step 5: Commit the schema boundary**

```bash
git add service/sqls/develop/develop.sql service/sqls/sql_contract_test.go service/internal/dbtable
git commit -m "feat(service): define content schema"
```

---

### Task 2: Validate GFS Configuration and Pin the Local Signing Contract

**Files:**
- Modify: `service/internal/config/config.go`
- Modify: `service/internal/config/config_test.go`
- Modify: `service/cmd/blog-admin/main_test.go`
- Modify: `service/cmd/blog-service/main_test.go`
- Modify: `service/internal/app/app_test.go`
- Modify: `service/tests/flow/auth_test.go`
- Create: `service/internal/randomkey/generator.go`
- Create: `service/internal/randomkey/generator_test.go`
- Create: `service/internal/media/gfs_signer.go`
- Create: `service/internal/media/gfs_signer_test.go`
- Create: `service/internal/media/gfs_client.go`
- Create: `service/internal/media/gfs_client_test.go`
- Create: `docs/contracts/gfs-blog-media.md`

**Interfaces:**
- Consumes: injected `io.Reader`, `func() time.Time`, `*http.Client`, and the deployed GFS contracts at commits `f9b8256` and `bcf8725`.
- Produces: `config.GFSConfig`, `randomkey.Generator`, `media.GFSSigner`, and `media.GFSClient` implementing `MetadataReader` and `ReadURLSigner`.

- [ ] **Step 1: Add failing direct/load configuration tests**

Extend `Config` with:

```go
type Config struct {
	Environment string
	HTTP HTTPConfig
	MySQL MySQLConfig
	Redis RedisConfig
	IDGen IDGenConfig
	Session SessionConfig
	GFS GFSConfig
}
type GFSConfig struct {
	BaseURL string
	AppID string
	AppSecret string
	PublicReadSecret string
}
```

Add cases for missing `BLOG_GFS_BASE_URL`, `BLOG_GFS_APP_ID`, `BLOG_GFS_APP_SECRET`, and `BLOG_GFS_PUBLIC_READ_SECRET`; base URLs with userinfo, query, fragment, non-root path, or trailing slash; and production HTTP base URL. Assert `Load` normalizes a root trailing slash away while direct `Validate` rejects noncanonical input. Put recognizable secret values into test config and assert no returned error contains them.

- [ ] **Step 2: Run config tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/config -run 'GFS|Validate|Load' -v
```

Expected: FAIL because the GFS fields and validation do not exist.

- [ ] **Step 3: Implement GFS config parsing without secret echoing**

Load the four exact environment variables, reuse an origin parser that accepts only canonical `http`/`https` origins, and require HTTPS in production. `Validate` names only the variable that failed. Do not derive or store a secret in an error. The configured `BLOG_GFS_APP_SECRET` is the raw value returned once by GFS app registration; the signer computes `md5(rawSecret)` because GFS stores and validates with that digest.

Update every existing valid config fixture and command environment map in config, app, bootstrap, flow, and process tests with non-secret test GFS values so unrelated foundation tests remain meaningful. Add no fallback values that would make a missing production GFS variable pass validation.

- [ ] **Step 4: Add failing random-key and fixed-vector signer tests**

Define:

```go
type Generator struct { reader io.Reader; mu sync.Mutex }
func New(reader io.Reader) (*Generator, error)
func (g *Generator) ArticleSlug() (string, error)
func (g *Generator) TagSlug() (string, error)
func (g *Generator) MediaPublicKey() (string, error)
func (g *Generator) Nonce() (string, error)
```

Assert an article slug is 12 characters, a tag slug is `t_` plus 12 characters, a media key is `m_` plus 22 characters, and a nonce is 22 characters. The alphabet is exactly lowercase ASCII letters, digits, `-`, and `_`; fixed bytes produce deterministic vectors; nil receiver, nil reader, and a short/erroring reader return sanitized errors without panic.

Define signer methods:

```go
func NewGFSSigner(baseURL, appID, rawAppSecret, publicReadSecret string, keys *randomkey.Generator) (*GFSSigner, error)
func (s *GFSSigner) UploadPolicy(now time.Time) (UploadPolicy, error)
func (s *GFSSigner) ReadURL(item Media, now time.Time) (string, error)
```

Test the exact GFS formulas:

```text
uploadSecret = lowercase-hex MD5(raw app secret)
uploadSignature = lowercase-hex MD5(appId + "_" + policy + "_" + unixTimestamp + "_60_" + nonce + "_" + uploadSecret)
readSignature = lowercase-hex MD5(publicReadSecret + "_" + policy + "_" + unixTimestamp + "_60_" + publicReadSecret)
```

Decode upload policy with standard padded Base64 and assert its only JSON property is the fixed `savePath`. Decode the file-ID-91 read policy and assert `{userId:"",fileId:91,imageWidth:0,imageHeight:0,internalFlag:0}`. Assert returned URLs contain escaped values and errors never contain either secret or a generated signature.

- [ ] **Step 5: Run signer tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/randomkey ./internal/media -run 'Random|Key|Signer|Policy|ReadURL' -v
```

Expected: FAIL because the generator and signer are absent.

- [ ] **Step 6: Implement the random generator and exact local signer**

Read one random byte per output character under the mutex and mask to six bits into the 64-character alphabet. Return stable configuration/random-source errors without byte values. Build GFS JSON with structs, `json.Marshal`, and `base64.StdEncoding`; build query strings with `net/url`, never string concatenation of unescaped values. For configured base `https://gfs.example.com`, set `UploadURL` to `https://gfs.example.com/v1/upload`, `Expire` to `"60"`, and `FileField` to `"file"`.

- [ ] **Step 7: Add failing GFS metadata client tests**

Use `httptest.Server` to prove `Metadata(ctx, 41)` requests exactly `GET /alioss/objects/41/metadata`, accepts only HTTP 200 plus GFS envelope `code:0`, verifies `data.fileId == 41`, maps image width/height and actual content type/size, applies a 5-second client timeout supplied by the caller, bounds the response body to 64 KiB, and maps timeout, malformed JSON, oversized JSON, nonzero GFS code, and mismatched IDs to `ErrDependencyUnavailable` without leaking response bodies or request URLs.

- [ ] **Step 8: Implement the client and GFS contract document**

Implement:

```go
func NewGFSClient(baseURL string, client *http.Client) (*GFSClient, error)
func (c *GFSClient) Metadata(ctx context.Context, fileID int64) (Metadata, error)
```

Parse `data.imageMetadata.imageWidth.value` and `data.imageMetadata.imageHeight.value` as positive decimal integers and treat missing/non-decimal values as an unavailable dependency. Document in `docs/contracts/gfs-blog-media.md` the fixed multipart fields `file`, `appId`, `policy`, `signature`, `timestamp`, `expire`, and `nonce`; the successful upload envelope `data.val` and string-valued `data.objectInfo`; the metadata endpoint/envelope and nested string-valued image dimensions; the two signing formulas; the fixed blog path; private OSS requirement; and the two required GFS commits. State that the service never sends a read-signing request to GFS and that GFS's final OSS hop must remain 302/307.

- [ ] **Step 9: Run focused tests and the read-only GFS compatibility checks**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/config ./internal/randomkey ./internal/media -run 'GFS|Random|Key|Signer|Policy|ReadURL|Metadata' -v
git -C /Users/qiuxs/codes/qiuxs/go-file-server merge-base --is-ancestor f9b8256 HEAD
git -C /Users/qiuxs/codes/qiuxs/go-file-server merge-base --is-ancestor bcf8725 HEAD
cd /Users/qiuxs/codes/qiuxs/go-file-server
GOTOOLCHAIN=go1.24.4 go test ./middlewares/mygin/redirect -run TestTemporaryUsesFoundStatus -v
```

Expected: PASS for all focused tests and both ancestry checks. The GFS command is read-only verification of that repository; do not commit there.

- [ ] **Step 10: Commit the GFS contract boundary**

```bash
git add service/internal/config service/internal/randomkey service/internal/media/gfs_signer.go service/internal/media/gfs_signer_test.go service/internal/media/gfs_client.go service/internal/media/gfs_client_test.go service/cmd/blog-admin/main_test.go service/cmd/blog-service/main_test.go service/internal/app/app_test.go service/tests/flow/auth_test.go docs/contracts/gfs-blog-media.md
git commit -m "feat(service): add gfs contract adapter"
```

---

### Task 3: Implement Stable Tag Management and Revision Snapshots

**Files:**
- Create: `service/internal/tag/model.go`
- Create: `service/internal/tag/repository.go`
- Create: `service/internal/tag/repository_mysql.go`
- Create: `service/internal/tag/repository_mysql_test.go`
- Create: `service/internal/tag/service.go`
- Create: `service/internal/tag/service_test.go`

**Interfaces:**
- Consumes: `dbtable.Tags`, shared `*idgen.Generator`, `*randomkey.Generator`, `*sql.DB`, and injected UTC clock.
- Produces: `tag.Repository` plus service methods `Create`, `List`, `Rename`, and `Snapshots` for HTTP and revision orchestration.

- [ ] **Step 1: Add failing tag service tests**

Define the service surface:

```go
type Service interface {
	Create(context.Context, string) (Tag, error)
	List(context.Context) ([]Tag, error)
	Rename(context.Context, int64, string) (Tag, error)
	Snapshots(context.Context, []int64) ([]Snapshot, error)
}
func NewService(Repository, *randomkey.Generator, func() time.Time) (Service, error)
```

With a fake repository, assert names are trimmed and internal whitespace collapsed, empty or over-64-rune names are rejected, duplicate IDs are rejected, repository results are reordered to the caller's `tagIds`, and a missing requested ID returns `tag.ErrNotFound`. Assert create retries a random slug conflict at most five times, while rename changes only the normalized name and preserves the stored slug. Assert nil repository, random generator, or clock fails construction without panic.

- [ ] **Step 2: Run service tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/tag -run 'Service|Normalize|Snapshot' -v
```

Expected: FAIL because the tag model and service do not exist.

- [ ] **Step 3: Implement tag behavior minimally**

Normalize with `strings.Fields` joined by one space and count Unicode code points. Generate a stable `t_` random slug only during create. Return snapshots as `{TagID,Name,Slug,Position}` in the submitted order; never rebuild a historic snapshot during version copy or restore.

- [ ] **Step 4: Add failing sqlmock repository tests**

Cover these exact statements and outcomes:

```sql
INSERT INTO tags (id, name, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
SELECT id, name, slug, created_at, updated_at FROM tags ORDER BY name ASC, id ASC
SELECT id, name, slug, created_at, updated_at FROM tags WHERE id IN (?, ?) ORDER BY id ASC
UPDATE tags SET name = ?, updated_at = ? WHERE id = ?
SELECT id, name, slug, created_at, updated_at FROM tags WHERE id = ?
```

Assert insert uses the shared generator's `idseq:tags` counter, preserves signed `int64`, maps only named `uk_tags_name` and `uk_tags_slug` MySQL 1062 errors to domain conflicts, maps zero update rows to `ErrNotFound`, and never orders by ID as a time surrogate.

- [ ] **Step 5: Run repository tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/tag -run 'MySQL|Repository' -v
```

Expected: FAIL because `NewMySQLRepository` and SQL methods are absent.

- [ ] **Step 6: Implement the MySQL adapter and verify the package**

Implement `NewMySQLRepository(db *sql.DB, ids *idgen.Generator) *MySQLRepository` with stable initialization errors. Use `ids.Insert(ctx, dbtable.Tags, insert)` for create; never instantiate a counter or generator in the package. Validate positive IDs before query construction, build only `?` bind markers for `IN`, and wrap dependency errors without names, slugs, or SQL argument values.

Run:

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/tag -v
```

Expected: PASS.

- [ ] **Step 7: Commit tag management**

```bash
git add service/internal/tag
git commit -m "feat(service): add stable tag management"
```

---

### Task 4: Issue Upload Policies and Register Verified Media

**Files:**
- Create: `service/internal/media/model.go`
- Create: `service/internal/media/repository.go`
- Create: `service/internal/media/repository_mysql.go`
- Create: `service/internal/media/repository_mysql_test.go`
- Create: `service/internal/media/service.go`
- Create: `service/internal/media/service_test.go`

**Interfaces:**
- Consumes: `GFSSigner`, `MetadataReader`, shared `*idgen.Generator`, shared `*randomkey.Generator`, `*sql.DB`, and injected UTC clock.
- Produces: `media.Service.IssueUploadPolicy`, `Register`, active media lookup, and revision reference resolution.

- [ ] **Step 1: Add failing service tests for policy and metadata rules**

Define:

```go
type Service interface {
	IssueUploadPolicy(context.Context) (UploadPolicy, error)
	Register(context.Context, int64, string) (Media, error)
	ResolveReferences(context.Context, *int64, []string) (*Media, []Reference, error)
	RequireActive(context.Context, int64) error
	FindActiveByPublicKey(context.Context, string) (Media, error)
}
func NewService(Repository, MetadataReader, *GFSSigner, *randomkey.Generator, func() time.Time) (Service, error)
```

Use fakes to assert policy issuance does not accept any caller path and uses the injected clock. For registration, the request contains only positive `gfsFileId` and `originalName`; the service fetches metadata itself and requires response ID equality plus exact normalized filename equality with GFS `data.fileName`. Table-test JPEG (`.jpg` and `.jpeg`), PNG, WebP, and GIF success; empty/basename-only-invalid names, path components, NUL, browser/GFS filename mismatch, SVG, extension/MIME mismatch, zero/negative/over-10-MiB size, zero/negative/over-12000 dimensions, and upstream failures. Assert an existing `gfs_file_id` is idempotently returned without a second INSERT, and a public-key unique conflict retries five times before returning a sanitized error.

For `ResolveReferences`, assert cover and content media must be active, duplicate body keys preserve first appearance only, cover purpose is `cover` at position 0, body purpose is `content` beginning at position 1 when a cover exists and position 0 otherwise, missing keys reject the entire save, and no numeric GFS ID is accepted from Markdown.

- [ ] **Step 2: Run service tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/media -run 'Service|Register|Resolve|Upload' -v
```

Expected: FAIL at missing service/model symbols.

- [ ] **Step 3: Implement media validation and orchestration**

Normalize `originalName` with `path.Base` equality, lowercase its final extension, and compare it to this fixed map:

```go
var allowedExtensions = map[string]map[string]struct{}{
	"image/jpeg": {".jpg": {}, ".jpeg": {}},
	"image/png":  {".png": {}},
	"image/webp": {".webp": {}},
	"image/gif":  {".gif": {}},
}
```

Persist GFS `data.fileName` plus actual metadata values, not browser-supplied size/MIME/dimensions. Store `state="active"`. Registration returns `/img/proxy/{publicKey}` only through the HTTP view; the domain object stores the key alone.

- [ ] **Step 4: Add failing media repository tests**

Cover these exact query families:

```sql
INSERT INTO media (id, public_key, gfs_file_id, original_name, mime_type, file_size, width, height, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)
SELECT id, public_key, gfs_file_id, original_name, mime_type, file_size, width, height, state, created_at, updated_at FROM media WHERE gfs_file_id = ?
SELECT id, public_key, gfs_file_id, original_name, mime_type, file_size, width, height, state, created_at, updated_at FROM media WHERE id IN (?, ?) AND state = 'active'
SELECT id, public_key, gfs_file_id, original_name, mime_type, file_size, width, height, state, created_at, updated_at FROM media WHERE public_key IN (?, ?) AND state = 'active'
SELECT id, public_key, gfs_file_id, original_name, mime_type, file_size, width, height, state, created_at, updated_at FROM media WHERE public_key = ? AND state = 'active'
```

Assert `ids.Insert(ctx, dbtable.Media, insert)`, `sql.ErrNoRows` to `ErrNotFound`, named unique-key mapping, positive input validation, deterministic result order reconstructed by the service, and sanitized errors.

- [ ] **Step 5: Run repository tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/media -run 'MySQL|Repository' -v
```

Expected: FAIL because the MySQL adapter is missing.

- [ ] **Step 6: Implement the repository and run all media tests**

Implement `NewMySQLRepository(db *sql.DB, ids *idgen.Generator) *MySQLRepository`. Match duplicate errors only for `uk_media_public_key` and `uk_media_gfs_file_id`; do not treat other 1062 errors as idempotence or retryable public-key conflicts.

Run:

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/media -v
```

Expected: PASS.

- [ ] **Step 7: Commit verified media registration**

```bash
git add service/internal/media
git commit -m "feat(service): register verified media"
```

---

### Task 5: Create Stable Article Identities and Enforce Trash Lifecycle

**Files:**
- Create: `service/internal/article/model.go`
- Create: `service/internal/article/repository.go`
- Create: `service/internal/article/repository_mysql.go`
- Create: `service/internal/article/repository_mysql_test.go`
- Create: `service/internal/article/service.go`
- Create: `service/internal/article/service_test.go`

**Interfaces:**
- Consumes: shared `*idgen.Generator`, shared `*randomkey.Generator`, `revision.DraftReader`, `*sql.DB`, and injected UTC clock.
- Produces: immutable article slug creation, explicit-time list ordering, detail assembly, and active/trashed transitions.

- [ ] **Step 1: Add failing article service tests**

Define:

```go
type Detail struct { Article Article; Draft revision.Draft }
type Service interface {
	Create(context.Context) (Detail, error)
	Get(context.Context, int64) (Detail, error)
	List(context.Context, State) ([]Summary, error)
	Trash(context.Context, int64) error
	Untrash(context.Context, int64) error
}
func NewService(Repository, DraftReader, *randomkey.Generator, func() time.Time) (Service, error)
```

Assert create generates a 12-character slug and retries only `ErrSlugConflict` up to five times. Assert Get combines the article and current draft. Assert list only accepts `active` or `trashed`. Trash rejects an article whose `PublishedRevisionID` is non-nil before any update; unpublished active articles transition to trashed; untrash transitions only trashed to active; repeated or racing state changes return `ErrStateConflict`. Assert nil dependencies fail construction without panic.

- [ ] **Step 2: Run article service tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/article -run 'Service|Create|Trash|Untrash' -v
```

Expected: FAIL because the article package is absent.

- [ ] **Step 3: Implement article service behavior**

Keep the slug immutable after repository creation. Do not add an endpoint or method that edits it. Do not manufacture a recent-publish field in Stage 2; the list exposes `publishedRevisionId`/`online` and leaves publish job status for Stage 3.

- [ ] **Step 4: Add failing transaction and list repository tests**

`Create` is one MySQL transaction:

```sql
INSERT INTO articles (id, slug, draft_revision_id, published_revision_id, state, created_at, updated_at) VALUES (?, ?, NULL, NULL, 'active', ?, ?)
INSERT INTO article_revisions (id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at) VALUES (?, ?, 1, 'editing', 'draft', '', '', NULL, '', ?, 1, ?, ?)
UPDATE articles SET draft_revision_id = ?, updated_at = ? WHERE id = ? AND draft_revision_id IS NULL
```

Assert `idseq:articles` and `idseq:article_revisions` each advance through the same injected generator, both rows roll back on any failure, and only a named `uk_articles_slug` conflict maps to `ErrSlugConflict`. The empty content hash is produced by `revision.ComputeHash`, not a SQL literal.

Cover:

```sql
SELECT id, slug, draft_revision_id, published_revision_id, state, created_at, updated_at FROM articles WHERE id = ?
SELECT a.id, a.slug, a.draft_revision_id, a.published_revision_id, a.state, a.created_at, a.updated_at, r.title, r.updated_at FROM articles a JOIN article_revisions r ON r.id = a.draft_revision_id WHERE a.state = ? ORDER BY r.updated_at DESC, a.id ASC
UPDATE articles SET state = 'trashed', updated_at = ? WHERE id = ? AND state = 'active' AND published_revision_id IS NULL
UPDATE articles SET state = 'active', updated_at = ? WHERE id = ? AND state = 'trashed'
```

Assert null published revision scanning, explicit timestamp ordering, zero rows to state conflict, missing rows to not found, and rollback/commit expectations. The trash condition must include `published_revision_id IS NULL`, so a publish racing the service's preliminary read cannot be hidden. After a zero-row trash result, reload the article: map a now-published row to `ErrMustBeUnpublished` and any other mismatch to `ErrStateConflict`.

- [ ] **Step 5: Run repository tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/article -run 'MySQL|Repository|Transaction|List' -v
```

Expected: FAIL because persistence is absent.

- [ ] **Step 6: Implement persistence and verify the package**

Implement `NewMySQLRepository(db *sql.DB, ids *idgen.Generator) *MySQLRepository`. Use `ids.Insert` with `dbtable.Articles` and `dbtable.ArticleRevisions` inside the transaction; do not create a package-local generator. Keep pointer updates conditional so a partial/racing create cannot silently replace the draft.

Run:

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/article -v
```

Expected: PASS.

- [ ] **Step 7: Commit article identity and lifecycle**

```bash
git add service/internal/article
git commit -m "feat(service): add article lifecycle"
```

---

### Task 6: Autosave the Editing Revision with Optimistic Locking

**Files:**
- Create: `service/internal/revision/model.go`
- Create: `service/internal/revision/markdown.go`
- Create: `service/internal/revision/markdown_test.go`
- Create: `service/internal/revision/hash.go`
- Create: `service/internal/revision/hash_test.go`
- Create: `service/internal/revision/repository.go`
- Create: `service/internal/revision/repository_mysql.go`
- Create: `service/internal/revision/repository_mysql_test.go`
- Create: `service/internal/revision/service.go`
- Create: `service/internal/revision/service_test.go`
- Modify: `service/go.mod`
- Modify: `service/go.sum`

**Interfaces:**
- Consumes: `tag.TagResolver`, `media.MediaResolver`, shared `*idgen.Generator`, `*sql.DB`, and injected UTC clock.
- Produces: `revision.Service.GetDraft`, `SaveDraft`, `Preview`, `ValidateFreezable`, and a transactional optimistic-lock repository.

- [ ] **Step 1: Add failing Markdown policy and hash tests**

Add goldmark `v1.8.5` and parse with GFM extensions for AST inspection only. Test that ordinary GFM headings, tables, task lists, fenced code, and autolinks are accepted; raw HTML nodes are rejected; image destinations with `/img/proxy/` followed by `m_` and exactly 22 allowed key characters are returned in first-seen order with duplicates removed; external image URLs, malformed proxy keys, and proxy-looking text in fenced/inline code are not treated as registered references. Reject input over 2 MiB, titles over 200 runes, and summaries over 600 runes.

Define:

```go
func ValidateDraft(Content) ([]string, error)
func ValidateFreezable(Content) error
func ComputeHash(PreparedContent) string
```

`ValidateDraft` allows `blob:` destinations but does not return them as references. `ValidateFreezable` additionally rejects any image/link destination with the `blob:` scheme and requires a nonblank title. Hash a JSON-encoded private struct with fixed field order containing normalized title/summary, cover public key or null, exact Markdown bytes, and ordered `{tagId,name,slug}` snapshots; return 64 lowercase hex SHA-256 characters. Test repeatability, field sensitivity, tag-order sensitivity, and no dependence on revision ID/time/lock version.

- [ ] **Step 2: Run Markdown/hash tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go get github.com/yuin/goldmark@v1.8.5
GOTOOLCHAIN=go1.25.7 go test ./internal/revision -run 'Markdown|Validate|Hash|Media' -v
```

Expected: FAIL because validation and hashing are not implemented.

- [ ] **Step 3: Implement validation, extraction, and canonical hash**

Walk goldmark nodes rather than applying a body-wide regular expression. Reject `ast.KindRawHTML`; inspect `ast.Image` destinations and links for transient blobs. A relative image URL is supported only when it has no query/fragment and its complete path is `/img/proxy/{validPublicKey}`. Keep Markdown source unchanged in storage.

- [ ] **Step 4: Add failing revision service tests**

Define:

```go
type Service interface {
	GetDraft(context.Context, int64) (Draft, error)
	SaveDraft(context.Context, int64, int64, Content) (Draft, error)
	Preview(context.Context, int64) (Draft, error)
	CreateVersion(context.Context, int64, int64) (Version, Draft, error)
	ListVersions(context.Context, int64) ([]Version, error)
	RestoreVersion(context.Context, int64, int64, int64) (Draft, error)
	ValidateFreezable(Draft) error
}
func NewService(Repository, TagResolver, MediaResolver, func() time.Time) (Service, error)
```

For SaveDraft assert: input lock version must be positive; tag IDs resolve to ordered current snapshots; body keys and optional cover resolve to active media; the prepared hash includes the resolved cover key; validation/resolution failure performs no repository write; repository `ErrConflict` remains distinguishable; and the successful returned draft has `lockVersion = submitted + 1`. Preview returns the current complete draft without freezing it. Assert constructors reject nil dependencies safely.

- [ ] **Step 5: Run service tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/revision -run 'Service|SaveDraft|Preview' -v
```

Expected: FAIL because orchestration is absent.

- [ ] **Step 6: Implement the service preparation pipeline**

Run validation, tag resolution, media resolution, and hashing before repository mutation. Convert dependency-specific missing media/tag results to `ErrInvalidContent`, preserve `ErrConflict`, and wrap operational errors without content, tag names, keys, or GFS values.

- [ ] **Step 7: Add failing sqlmock optimistic-save tests**

`GetDraft` queries the article's current editing row, then ordered snapshots and media references. `SaveDraft` uses one transaction and starts with:

```sql
UPDATE article_revisions SET title = ?, summary = ?, cover_media_id = ?, content_md = ?, content_hash = ?, lock_version = lock_version + 1, updated_at = ? WHERE article_id = ? AND status = 'editing' AND lock_version = ?
```

If affected rows are zero, roll back and return `ErrConflict`; do not delete/reinsert associations. On success:

```sql
SELECT id, lock_version, revision_no, created_at FROM article_revisions WHERE article_id = ? AND status = 'editing'
DELETE FROM article_revision_tags WHERE revision_id = ?
INSERT INTO article_revision_tags (id, revision_id, tag_id, tag_name, tag_slug, position, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)
DELETE FROM article_revision_media WHERE revision_id = ?
INSERT INTO article_revision_media (id, revision_id, media_id, purpose, position, created_at) VALUES (?, ?, ?, ?, ?, ?)
UPDATE articles SET updated_at = ? WHERE id = ? AND state = 'active'
```

Assert all association IDs come from shared `idseq:article_revision_tags` and `idseq:article_revision_media`; insert order follows tag positions then cover/content order; any Redis/SQL failure rolls back; only editing rows can change; frozen rows are never targeted; zero active-article update rows roll back as `ErrArticleInactive`; commit returns the submitted content with database revision identity and incremented lock. Add nil/zero adapter tests.

- [ ] **Step 8: Run repository tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/revision -run 'MySQL|Repository|Save|Conflict|Rollback' -v
```

Expected: FAIL because the MySQL adapter is absent.

- [ ] **Step 9: Implement atomic save and verify revision tests**

Implement `NewMySQLRepository(db *sql.DB, ids *idgen.Generator) *MySQLRepository`. Use constants from `dbtable` for every generated association ID. Sort versions only in version methods; preserve request order for tags and first-use order for media. Never update association rows belonging to frozen revisions.

Run:

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/revision -v
```

Expected: PASS.

- [ ] **Step 10: Commit optimistic draft persistence**

```bash
git add service/internal/revision service/go.mod service/go.sum
git commit -m "feat(service): autosave article drafts"
```

---

### Task 7: Freeze Manual Versions and Restore History as New Drafts

**Files:**
- Modify: `service/internal/revision/repository_mysql_test.go`
- Modify: `service/internal/revision/repository_mysql.go`
- Modify: `service/internal/revision/service_test.go`
- Modify: `service/internal/revision/service.go`

**Interfaces:**
- Consumes: the editing-draft repository, shared association ID generator, `ValidateFreezable`, and current UTC clock.
- Produces: immutable manual versions, ordered history, and non-destructive restore.

- [ ] **Step 1: Add failing service tests for freeze and restore**

Assert `CreateVersion` loads the current draft, calls `ValidateFreezable`, and performs no repository write for blank title, transient blob URL, unresolved/inactive media, or raw HTML. Assert success returns the old content as frozen `manual_version` and a new editing draft with a new ID, `revisionNo + 1`, and `lockVersion = 1`.

Assert `RestoreVersion(articleID, revisionID, lockVersion)` accepts only a frozen version belonging to the same article, freezes the current editing draft as a manual version so no work is lost, copies the selected historic content/tag snapshots/media references into a new editing row, and never re-resolves old tag names/slugs from the current tag table. The selected historic row remains byte-for-byte unchanged. Stale current lock returns `ErrConflict` before inserts.

- [ ] **Step 2: Run service tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/revision -run 'CreateVersion|RestoreVersion|Freezable' -v
```

Expected: FAIL because version methods do not implement these rules.

- [ ] **Step 3: Implement service-level freeze validation and delegation**

Reuse the exact same `ValidateFreezable` method that Stage 3 publishing will consume. Do not create a weaker version-only validator. Preserve repository domain errors for HTTP mapping.

- [ ] **Step 4: Add failing transaction tests for manual version creation**

In one transaction:

1. `SELECT` the current editing row `FOR UPDATE` and compare `lock_version`.
2. Query its ordered tag/media associations.
3. `UPDATE article_revisions SET status='frozen', reason='manual_version', updated_at=? WHERE id=? AND status='editing' AND lock_version=?`.
4. Allocate and insert a new editing revision with `revision_no + 1`, the same content/hash, and lock version 1.
5. Allocate/copy association rows with their stored snapshot values.
6. `UPDATE articles SET draft_revision_id=?, updated_at=? WHERE id=? AND draft_revision_id=? AND state='active'`.
7. Commit.

Assert any failure rolls back, stale revision conditions return `ErrConflict`, inactive article pointer replacement returns `ErrArticleInactive`, the old row is never updated after freezing, and pointer replacement is conditional.

- [ ] **Step 5: Add failing transaction tests for restore and history**

Restore first selects the target by both revision and article IDs with `status='frozen'`, then locks the current editing row. It freezes current content as `manual_version`, inserts a new editing revision using the selected target's scalar content/hash, allocates copies of the target's snapshot/reference rows, and conditionally moves `articles.draft_revision_id`. The new revision number is current editing `revision_no + 1`, not target revision number + 1 and not an ID-derived value.

History uses:

```sql
SELECT id, article_id, revision_no, status, reason, title, summary, cover_media_id, content_md, content_hash, lock_version, created_at, updated_at FROM article_revisions WHERE article_id = ? AND status = 'frozen' ORDER BY revision_no DESC
```

Load ordered snapshots/references for returned IDs and assert a later tag rename cannot alter stored historic `tag_name`/`tag_slug`.

- [ ] **Step 6: Run repository tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/revision -run 'CreateVersion|RestoreVersion|ListVersions|Immutable|Rollback' -v
```

Expected: FAIL on the first missing version transaction.

- [ ] **Step 7: Implement version transactions and verify the package**

Use small scan/copy helpers shared by create-version and restore, but keep the target historic row read-only. Do not delete history or add history cleanup.

Run:

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/revision -v
```

Expected: PASS, including optimistic save tests from Task 6.

- [ ] **Step 8: Commit revision history**

```bash
git add service/internal/revision
git commit -m "feat(service): add revision history restore"
```

---

### Task 8: Persist Publishable Site Settings and the Filing Gate

**Files:**
- Create: `service/internal/settings/model.go`
- Create: `service/internal/settings/repository.go`
- Create: `service/internal/settings/repository_mysql.go`
- Create: `service/internal/settings/repository_mysql_test.go`
- Create: `service/internal/settings/site_service.go`
- Create: `service/internal/settings/site_service_test.go`

**Interfaces:**
- Consumes: shared `*idgen.Generator`, active media lookup for default SEO image, `*sql.DB`, and injected UTC clock.
- Produces: `GetSite`, optimistic `PutSite`, and exported `ValidatePublishable` for Stage 3 release creation.

- [ ] **Step 1: Add failing site-default, validation, and gate tests**

Define:

```go
type SiteService interface {
	GetSite(context.Context) (Site, error)
	PutSite(context.Context, Site, int64) (Site, error)
}
type ActiveMediaValidator interface { RequireActive(context.Context, int64) error }
func NewSiteService(SiteRepository, ActiveMediaValidator, func() time.Time) (SiteService, error)
func DefaultSite() Site
func ValidatePublishable(Site) error
```

`DefaultSite` returns `SiteName:"qiuxs"`, `AuthorName:"qiuxs"`, filing name `长安休息室`, filing number `浙ICP备17057726号-1`, lock version 0, empty Markdown/status/social/SEO strings, nil default image, and no synthetic persistent ID. `GetSite` returns this virtual value only for `settings.ErrNotFound`.

Table-test nonblank site/author names; field rune limits 100/100/1000/500/2 MiB/100/300; at most 16 social links; nonblank unique labels; canonical HTTPS social URLs without userinfo; optional active SEO media; and nonblank filing fields. `ValidatePublishable` independently rejects blank/whitespace filing name or number and returns no field value in its error. `PutSite` expected lock 0 creates the first Redis-ID row; positive expected lock updates; stale versions return `ErrConflict`.

- [ ] **Step 2: Run site tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/settings -run 'Site|Default|Publishable|Filing' -v
```

Expected: FAIL because settings do not exist.

- [ ] **Step 3: Implement site validation and service orchestration**

Trim scalar display fields but preserve exact `AboutMD`. Return `FilingURL` only in the HTTP view; never persist a configurable filing URL. Validate the SEO media before persistence. Keep `ValidatePublishable` exported and free of repository dependencies.

- [ ] **Step 4: Add failing site repository tests**

Cover missing-row Get, first create through `idseq:site_settings`, named singleton conflict, and conditional update:

```sql
SELECT id, site_name, author_name, author_bio, home_status, about_md, social_links_json, seo_default_title, seo_default_description, seo_default_image_media_id, filing_name, filing_number, lock_version, updated_at FROM site_settings WHERE singleton_key = 1
INSERT INTO site_settings (id, singleton_key, site_name, author_name, author_bio, home_status, about_md, social_links_json, seo_default_title, seo_default_description, seo_default_image_media_id, filing_name, filing_number, lock_version, created_at, updated_at) VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
UPDATE site_settings SET site_name=?, author_name=?, author_bio=?, home_status=?, about_md=?, social_links_json=?, seo_default_title=?, seo_default_description=?, seo_default_image_media_id=?, filing_name=?, filing_number=?, lock_version=lock_version+1, updated_at=? WHERE singleton_key=1 AND lock_version=?
```

Assert valid JSON round-trips in order, invalid stored JSON returns a sanitized dependency error, zero update rows return `ErrConflict`, and no SQL inserts a fixed ID.

- [ ] **Step 5: Run repository tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/settings -run 'SiteRepository|SiteMySQL|Conflict' -v
```

Expected: FAIL because the adapter is absent.

- [ ] **Step 6: Implement the adapter and run settings tests**

Use `ids.Insert(ctx, dbtable.SiteSettings, insert)`, UTC timestamps, and strict JSON decoding. If two first writes race, map only `uk_site_settings_singleton` to `ErrConflict`; the client reloads and retries with the persisted lock.

Run:

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/settings -v
```

Expected: PASS for site tests.

- [ ] **Step 7: Commit site settings**

```bash
git add service/internal/settings
git commit -m "feat(service): add publishable site settings"
```

---

### Task 9: Replace Hotlink Policy Atomically and Invalidate Its Cache

**Files:**
- Modify: `service/internal/settings/model.go`
- Modify: `service/internal/settings/repository.go`
- Modify: `service/internal/settings/repository_mysql.go`
- Modify: `service/internal/settings/repository_mysql_test.go`
- Create: `service/internal/settings/hotlink.go`
- Create: `service/internal/settings/hotlink_test.go`

**Interfaces:**
- Consumes: shared `*idgen.Generator`, `HotlinkRepository`, injected UTC clock.
- Produces: admin `Get`/`Put`, exact Referer authorization, and a process-local cached `Current` policy invalidated immediately after successful writes.

- [ ] **Step 1: Add failing normalization, default, and cache tests**

Define:

```go
type HotlinkService interface {
	Get(context.Context) (HotlinkPolicy, error)
	Put(context.Context, bool, []HotlinkEntry) (HotlinkPolicy, error)
	Current(context.Context) (HotlinkPolicy, error)
	AllowsReferer(HotlinkPolicy, string) bool
}
func NewHotlinkService(HotlinkRepository, func() time.Time) (HotlinkService, error)
func NormalizeHostname(string) (string, error)
```

When no singleton row exists, return the virtual default `{AllowEmptyReferer:true, Entries:[qiuxs.com enabled, blog-admin.qiuxs.com enabled]}`. Accept ASCII DNS hostnames only; trim whitespace, lowercase, and remove one terminal dot. Reject schemes, ports, paths, wildcard labels, IP literals, empty labels, labels outside 1–63 characters, and total length over 253. Reject duplicates after normalization.

`AllowsReferer` permits empty only when configured. Nonempty Referer must be an absolute HTTP/HTTPS URL with a hostname; ignore its port/path/query while exact-matching the normalized hostname against enabled entries. Test subdomains do not match parent domains and malformed Referer denies.

Use a counting fake repository to prove first `Current` loads once, repeated reads use an immutable copy, failed Put keeps the prior cache, successful Put invalidates synchronously before returning, and the very next `Current` reloads. Do not cache dependency failures.

- [ ] **Step 2: Run hotlink service tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/settings -run 'Hotlink|Hostname|Referer|Cache' -v
```

Expected: FAIL because the policy service/cache is missing.

- [ ] **Step 3: Implement normalization, authorization, and cache invalidation**

Protect cache state with `sync.RWMutex`, clone entry slices on input/output, and use double-checked loading under the write lock. `Put` persists first, invalidates second, and returns the normalized stored policy. Holding the cache lock must not cover repository writes.

- [ ] **Step 4: Add failing atomic repository tests**

`GetHotlinkPolicy` reads the singleton and entries ordered `hostname ASC, id ASC`; if the singleton is missing, return `ErrNotFound` even if corrupt orphan entries exist. `ReplaceHotlinkPolicy` starts a transaction, creates the singleton through `idseq:hotlink_settings` when absent or updates it when present, deletes existing allowlist rows, allocates each new `referer_allowlist` ID from the shared generator, inserts normalized rows, and commits. Any counter/insert failure rolls back. Test an explicitly persisted empty list remains empty and does not regain virtual defaults.

Use these statement shapes:

```sql
SELECT id, allow_empty_referer FROM hotlink_settings WHERE singleton_key = 1
SELECT id, hostname, enabled FROM referer_allowlist ORDER BY hostname ASC, id ASC
UPDATE hotlink_settings SET allow_empty_referer=?, updated_at=? WHERE singleton_key=1
DELETE FROM referer_allowlist
INSERT INTO referer_allowlist (id, hostname, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
```

- [ ] **Step 5: Run repository tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/settings -run 'HotlinkRepository|ReplaceHotlink|Rollback' -v
```

Expected: FAIL because hotlink persistence is absent.

- [ ] **Step 6: Implement persistence and verify settings**

Use `dbtable.HotlinkSettings` and `dbtable.RefererAllowlist`; never assign singleton ID 1. Distinguish virtual defaults from an intentionally persisted empty allowlist by the existence of the singleton row.

Run:

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/settings -v
```

Expected: PASS.

- [ ] **Step 7: Commit immediate hotlink settings**

```bash
git add service/internal/settings
git commit -m "feat(service): add hotlink policy cache"
```

---

### Task 10: Serve the Public Referer-Aware Media Redirect

**Files:**
- Modify: `service/internal/media/model.go`
- Create: `service/internal/media/proxy.go`
- Create: `service/internal/media/proxy_test.go`
- Create: `service/internal/httpapi/media_proxy_handler.go`
- Create: `service/internal/httpapi/media_proxy_handler_test.go`
- Modify: `service/internal/httpapi/problem.go`

**Interfaces:**
- Consumes: `settings.HotlinkPolicyProvider`, Referer authorization, active media lookup, `media.ReadURLSigner`, and injected UTC clock.
- Produces: public `GET /img/proxy/{publicKey}` with sanitized 302/403/404/503 behavior and no byte proxying.

- [ ] **Step 1: Add failing proxy service tests**

Define:

```go
var ErrHotlinkForbidden = errors.New("hotlink forbidden")
type ProxyService interface { Redirect(context.Context, string, string) (string, error) }
func NewProxyService(settings.HotlinkPolicyProvider, interface {
	FindActiveByPublicKey(context.Context, string) (Media, error)
}, ReadURLSigner, func() time.Time) (ProxyService, error)
```

Assert authorization runs before media lookup so forbidden callers cannot probe key existence. Empty Referer follows the flag. An enabled exact hostname succeeds; a disabled host, subdomain, malformed URL, non-HTTP scheme, or unlisted host returns `ErrHotlinkForbidden`. Missing/inactive key returns `media.ErrNotFound`; policy/database/signing failures return `media.ErrDependencyUnavailable`; successful output is the locally computed short GFS URL. Assert nil dependencies and malformed keys fail safely and no error contains Referer, key, signature, or URL.

- [ ] **Step 2: Run proxy service tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/media -run 'Proxy|Redirect|Hotlink' -v
```

Expected: FAIL because `ProxyService` is absent.

- [ ] **Step 3: Implement authorization-first redirect resolution**

The service only returns a URL string; it never performs an HTTP request for read signing or file bytes. Call the injected signer with the actual GFS file ID from the active media row and current time.

- [ ] **Step 4: Add failing Gin handler tests**

Create the handler and route function:

```go
type MediaProxyHandler struct { service media.ProxyService }
func NewMediaProxyHandler(media.ProxyService) (*MediaProxyHandler, error)
func RegisterMediaProxy(router gin.IRoutes, handler *MediaProxyHandler)
func (h *MediaProxyHandler) Get(c *gin.Context)
```

Use a fake service and `httptest` to assert route shape, raw `Referer` forwarding, `302 Found` plus exact `Location`, empty body, and `Cache-Control: no-store`. Assert forbidden is `403 hotlink_forbidden`, missing/inactive is `404 not_found`, dependency failure is `503 dependency_unavailable`, malformed public key is `404`, and every response retains the request ID. Assert no response exposes a signed target or secret except the successful `Location` required by the browser.

- [ ] **Step 5: Run handler tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/httpapi -run 'MediaProxy|Hotlink|NoStore' -v
```

Expected: FAIL because the public handler and error mappings are absent.

- [ ] **Step 6: Implement the public handler and verify focused packages**

Set `Cache-Control` before validation. Use `c.Redirect(http.StatusFound, target)` only after success. Extend `problemMapping` by `errors.Is`, keep all titles generic, and do not add error details to `Problem`.

Run:

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/media ./internal/settings ./internal/httpapi -run 'Proxy|Hotlink|Referer|NoStore|Problem' -v
```

Expected: PASS.

- [ ] **Step 7: Commit the public media proxy**

```bash
git add service/internal/media service/internal/httpapi/media_proxy_handler.go service/internal/httpapi/media_proxy_handler_test.go service/internal/httpapi/problem.go
git commit -m "feat(service): add public media redirect"
```

---

### Task 11: Expand OpenAPI and Implement the Authenticated Admin Handlers

**Files:**
- Modify: `contracts/openapi/admin-v1.yaml`
- Modify: `service/internal/httpapi/contract_test.go`
- Create: `service/internal/httpapi/admin_handler.go`
- Create: `service/internal/httpapi/article_handler.go`
- Create: `service/internal/httpapi/tag_handler.go`
- Create: `service/internal/httpapi/media_handler.go`
- Create: `service/internal/httpapi/settings_handler.go`
- Create: `service/internal/httpapi/json.go`
- Create: `service/internal/httpapi/log_context.go`
- Create: `service/internal/httpapi/stage2_handler_test.go`
- Modify: `service/internal/httpapi/auth_handler.go`
- Modify: `service/internal/httpapi/problem.go`
- Generate: `service/internal/httpapi/admin.gen.go`

**Interfaces:**
- Consumes: article, revision, tag, media, site-settings, hotlink-settings, and existing auth services.
- Produces: one `AdminHandler` satisfying the expanded generated `ServerInterface`, with every Stage 2 operation authenticated and every unsafe operation Origin-protected by app composition.

- [ ] **Step 1: Add failing contract tests before editing YAML**

Load and validate the OpenAPI file, then assert every method/path/operation ID in the Fixed Domain and HTTP Contracts table. Walk all component properties named `id`, ending in `Id`, `articleId`, `revisionId`, `tagId`, `mediaId`, and `lockVersion`; assert integer values use `format: int64`, minimum 1 for identities, and minimum 0 only for the first-write settings lock. Assert every request schema has `additionalProperties:false`, every error response references `ProblemResponse`, and the public image route is absent from the Admin document.

Assert these exact request shapes:

```text
SaveDraftRequest: lockVersion,title,summary,coverMediaId,contentMd,tagIds
LockVersionRequest: lockVersion
CreateTagRequest: name
RenameTagRequest: name
RegisterMediaRequest: gfsFileId,originalName
PutSiteSettingsRequest: lockVersion plus all Site fields except id/updatedAt/filingUrl
PutHotlinkSettingsRequest: allowEmptyReferer,entries[{hostname,enabled}]
```

- [ ] **Step 2: Run contract tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/httpapi -run 'Contract|Stage2|IDFormat' -v
```

Expected: FAIL because Stage 2 paths and schemas are absent.

- [ ] **Step 3: Add the complete OpenAPI contract**

Keep the existing three auth operations unchanged. Add schemas `ArticleSummary`, `ArticleList`, `ArticleDetail`, `DraftView`, `PreviewView`, `RevisionView`, `RevisionList`, `VersionResult`, `TagView`, `TagList`, `MediaUploadPolicy`, `MediaView`, `SiteSettingsView`, `SocialLink`, `HotlinkEntry`, and `HotlinkSettingsView`. Use RFC3339 `date-time`, nullable `coverMediaId`/`publishedRevisionId`/SEO media, and exact enums. `MediaView.url` is `/img/proxy/{publicKey}`. Site output includes the constant read-only `filingUrl`.

Document `409` for stale draft/settings locks and illegal lifecycle changes, `422` for content/media validation, `503` for dependencies, `401` for every protected route, and `403` for unsafe Origin rejection.

- [ ] **Step 4: Generate code, run contract tests, and record the generated-interface RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 make generate
GOTOOLCHAIN=go1.25.7 go test ./internal/httpapi -run 'Contract|Stage2|IDFormat' -v
GOTOOLCHAIN=go1.25.7 go test ./internal/httpapi -run TestNonexistent -count=0
```

Expected: contract assertions PASS; package compilation FAILS because `AuthHandler` no longer implements the expanded `ServerInterface`. This compiler failure is the intentional second RED for handler implementation.

- [ ] **Step 5: Add failing handler behavior tests with domain fakes**

Define:

```go
type AdminHandler struct {
	auth *AuthHandler
	articles article.Service
	revisions revision.Service
	tags tag.Service
	media media.Service
	site settings.SiteService
	hotlink settings.HotlinkService
}
func NewAdminHandler(*AuthHandler, article.Service, revision.Service, tag.Service, media.Service, settings.SiteService, settings.HotlinkService) (*AdminHandler, error)
```

Move the compile-time assertion to `var _ ServerInterface = (*AdminHandler)(nil)` and make its three auth methods delegate unchanged. For each Stage 2 method, first call the existing private `requireAdmin`; anonymous and Session dependency failures must perform no domain call. Add representative success tests for every operation, strict unknown-field/wrong-content-type/oversized-body tests, positive path-ID checks, query enum checks, and response mapping tests.

Use body limits of 2 MiB for draft/site Markdown requests and 64 KiB for other JSON. Preserve submitted tag order. Map revision conflict to `409 revision_conflict`, settings conflict to `409 settings_conflict`, article transition races to `409 article_state_conflict`, published trash to `409 article_must_be_unpublished`, tag unique conflicts to `409 tag_conflict`, invalid Markdown/content to `422 invalid_content`, invalid metadata to `422 invalid_media`, domain not-found errors to `404 not_found`, and operational errors to `503 dependency_unavailable`. Never return internal error strings.

- [ ] **Step 6: Run handler tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/httpapi -run 'AdminHandler|Article|Draft|Version|Tag|Media|Settings' -v
```

Expected: FAIL because the composite handler and endpoint translations are missing.

- [ ] **Step 7: Implement the composite handlers**

Use explicit domain-to-generated-view mapping functions; do not JSON-marshal domain structs directly. Keep request decoding strict with one JSON object, `Content-Type: application/json` with optional UTF-8 charset, unknown fields rejected, and no trailing JSON. Reuse the existing auth cookie behavior unchanged. Every handler passes `c.Request.Context()` into services.

Set only numeric `admin_id` and `article_id` context attributes through helpers in `log_context.go`; article handlers set the validated path ID before the domain call. Do not attach tag/media public keys, request bodies, filenames, Referer, or GFS values to log context.

- [ ] **Step 8: Verify generation reproducibility and handler tests**

```bash
cd service
git add internal/httpapi/admin.gen.go
GOTOOLCHAIN=go1.25.7 make generate
git diff --exit-code -- internal/httpapi/admin.gen.go
GOTOOLCHAIN=go1.25.7 go test ./internal/httpapi -v
```

Expected: PASS; generation produces no second diff and all HTTP/contract tests pass.

- [ ] **Step 9: Commit the Admin API contract and handlers**

```bash
git add contracts/openapi/admin-v1.yaml service/internal/httpapi
git commit -m "feat(service): expose content admin api"
```

---

### Task 12: Compose One Shared Stack and Register Public/Admin Routes Correctly

**Files:**
- Modify: `service/internal/app/app.go`
- Modify: `service/internal/app/app_test.go`
- Modify: `service/cmd/blog-service/main.go`
- Modify: `service/cmd/blog-service/main_test.go`

**Interfaces:**
- Consumes: validated config, process-owned DB/Redis/logger/random/clock/HTTP client, all Stage 2 constructors, generated Admin routes, and public media handler.
- Produces: a Gin engine with correct middleware boundaries and no resource ownership changes.

- [ ] **Step 1: Add failing dependency and route-composition tests**

Extend dependencies:

```go
type Dependencies struct {
	DB *sql.DB
	Redis *redis.Client
	Logger *slog.Logger
	Random io.Reader
	Now func() time.Time
	HTTPClient *http.Client
}
```

Add Build tests for nil HTTP client and every invalid/missing GFS config category, asserting safe errors with no config values. Assert exact routes for all Stage 2 Admin operations and `GET /img/proxy/:publicKey`. Assert an anonymous Admin route returns 401, an unsafe authenticated request with the wrong/missing Origin returns 403, public media does not invoke auth/Origin middleware, unknown routes remain Problem JSON, and forwarded client IP remains untrusted.

Extend access-log tests to assert `admin_id` and `article_id` appear as JSON numbers after authenticated article handlers run, while cookies, body content, filenames, media keys, Referer, query strings, and signed targets remain absent. Continue logging only the Gin route template, not the raw stable-key path.

Add a test-only constructor observer or small unexported `buildComponents` return used by `app_test` to assert every Stage 2 MySQL repository receives the same `*idgen.Generator` pointer and every settings/media signer receives the same injected clock/random-key generator where specified. Do not expose this wiring seam outside `internal/app`.

- [ ] **Step 2: Run app tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/app -run 'Build|Stage2|Routes|Dependencies|Shared' -v
```

Expected: FAIL because HTTP client/GFS wiring and routes are absent.

- [ ] **Step 3: Compose the Stage 2 stack once**

In `Build`, after validation:

1. Construct one Redis counter, one `*idgen.Generator`, and one concurrency-safe random-key generator.
2. Construct tag, media, article, revision, and settings repositories with the same ID generator pointer.
3. Construct the GFS client/signer with validated config, injected HTTP client, random keys, and clock.
4. Construct repositories first, then the revision service, then the article service through the narrow `DraftReader` interface; keep the package dependency one-way from `article` to `revision`.
5. Construct hotlink service once and inject that same cache-bearing value into settings handlers and media proxy.
6. Construct the existing auth stack unchanged, then one composite `AdminHandler`.
7. Keep middleware order `RequestID`, access log, recovery. Keep `SetTrustedProxies(nil)`.
8. Register health routes. Register `/img/proxy` directly on the root engine. Register generated Admin handlers on the zero-prefix group with `OriginGuard` then `LoadAdminSession`.
9. Keep `NoRoute` last.

Do not close DB, Redis, or HTTP transports in Build. Do not query or mutate settings at startup; defaults load on first request.

- [ ] **Step 4: Add failing process HTTP-client tests**

In main tests, assert `run` passes one `*http.Client{Timeout:5*time.Second}` to Build, does not close DB/Redis differently, retains exact server timeouts and 30-second graceful shutdown, and never reads or executes `sqls/develop/develop.sql`. Keep sanitized startup errors.

- [ ] **Step 5: Run process tests and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./cmd/blog-service -run 'HTTPClient|Run|Shutdown|SQL' -v
```

Expected: FAIL because runtime composition does not provide the HTTP client.

- [ ] **Step 6: Implement process injection and verify composition**

Create the HTTP client in `main`; Build owns neither the client nor its transport. Do not add a GFS readiness probe because metadata availability must not make the text service unready.

Run:

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/app ./cmd/blog-service -v
```

Expected: PASS.

- [ ] **Step 7: Commit application composition**

```bash
git add service/internal/app service/cmd/blog-service
git commit -m "feat(service): wire content media stack"
```

---

### Task 13: Prove the In-Process Content, Revision, Media, and Hotlink Flow

**Files:**
- Create: `service/tests/flow/content_media_test.go`
- Modify: `service/tests/flow/auth_test.go`

**Interfaces:**
- Consumes: real `app.Build`, real Gin middleware/handlers/domain services/repositories/signers, sqlmock, miniredis, deterministic random source/clock, and fake GFS `httptest.Server`.
- Produces: one coherent Stage 2 acceptance flow with no deployed dependency.

- [ ] **Step 1: Write the failing end-to-end router flow**

Build a harness that prepares one real Argon2id admin hash, sqlmock DB, miniredis, deterministic random stream, fixed UTC clock, and fake GFS metadata server. Use real login to obtain the Secure cookie and manually attach it to HTTP requests because `httptest` HTTP does not automatically resend Secure cookies. Add `Origin` on every unsafe Admin request.

Drive this exact sequence and assert response bodies plus SQL/Redis effects:

1. Login and retain the existing auth guarantees.
2. Request a media upload policy; decode Base64 and assert only the fixed blog `savePath`, 60-second expiry, and no secret in response/logs.
3. Have fake GFS return actual PNG metadata for file ID 91; register media; assert signed BIGINT media ID, random `m_` public key, stable `/img/proxy/{key}`, and `idseq:media` advancement.
4. Create tag `Go`; assert stable `t_` slug and `idseq:tags` advancement.
5. Create an article; assert independent `idseq:articles`/`idseq:article_revisions`, 12-character slug, active state, empty editing revision number 1 and lock 1.
6. Save a GFM draft containing the stable image URL, cover media ID, and tag ID at lock 1; assert lock 2, media/tag associations, snapshot values, and deterministic content hash.
7. Repeat save with stale lock 1; assert `409 revision_conflict`, transaction rollback, and unchanged draft.
8. Create a manual version at lock 2; assert frozen version 1 and new editing revision 2 at lock 1.
9. Rename `Go` to `Golang`; assert stable tag slug, then list versions and assert version 1 still says `Go`.
10. Save changed revision 2 with current `Golang` snapshot, then restore frozen version 1; assert current revision 2 is preserved as another frozen manual version, new editing revision 3 copies historic `Go`, and the source version is unchanged.
11. Read preview and assert slug, title, exact Markdown, historic tag snapshot, cover, and proxy URL.
12. Trash and untrash the unpublished article; add a repository fixture with non-null `published_revision_id` and assert trash returns `409 article_must_be_unpublished` without state update.
13. GET the stable media URL with empty Referer under virtual defaults; assert `302`, `no-store`, and a locally signed GFS read URL without a signing HTTP call.
14. PUT hotlink settings with `allowEmptyReferer:false` and only `blog-admin.qiuxs.com`; immediately repeat empty-Referer GET and assert `403` without restarting, then use `Referer:https://blog-admin.qiuxs.com/preview` and assert `302`; use a subdomain and assert `403`.
15. GET and PUT site settings; assert default filing values/link, optimistic lock advancement, fixed link rejection from input, and stale update conflict.

Set an admin lookup expectation before each authenticated request because the real Session middleware reloads the active admin from MySQL. Use helper functions for those repetitive expectations, revision row fixtures, and association inserts, but do not replace Gin, auth, domain services, repositories, signer, or router with mocks.

- [ ] **Step 2: Run the flow and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./tests/flow -run TestContentRevisionMediaAndHotlinkFlow -v
```

Expected: FAIL at the first incomplete Stage 2 interaction. A missing sqlmock expectation is not accepted as functional RED; correct the harness until failure identifies missing/incorrect application behavior.

- [ ] **Step 3: Make only integration corrections revealed by the flow**

Correct route wiring, transaction order, DTO mapping, or test fixtures. Do not weaken production Argon2id, cookie security, Origin checking, MIME limits, optimistic locks, or GFS signatures to make the flow pass. Keep fake GFS request counts and `mock.ExpectationsWereMet()` assertions at the end.

- [ ] **Step 4: Run both flow suites repeatedly**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./tests/flow -run TestContentRevisionMediaAndHotlinkFlow -count=3 -v
GOTOOLCHAIN=go1.25.7 go test ./tests/flow -v
```

Expected: PASS three times without order/flakiness and PASS alongside the original auth flow.

- [ ] **Step 5: Commit the acceptance flow**

```bash
git add service/tests/flow service/internal/app service/internal/httpapi service/internal/article service/internal/revision service/internal/tag service/internal/media service/internal/settings
git commit -m "test(service): prove content media flow"
```

---

### Task 14: Document Operations and Run the Complete Stage 2 Gate

**Files:**
- Modify: `service/README.md`
- Modify: `README.md`
- Review: every file in the Planned File Map

**Interfaces:**
- Consumes: completed Stage 2 service, manual SQL lifecycle, GFS contract, and exact toolchain/build rules.
- Produces: operator/developer documentation and final evidence before Stage 3 work begins.

- [ ] **Step 1: Add a failing documentation contract test**

Extend an existing lightweight contract test or add `service/internal/app/docs_test.go` that reads `service/README.md` and asserts it documents all four GFS variables, the manual-only `develop.sql` order, no automatic migrations, GFS required commits, fixed upload path/limits, private bucket, health/start/generate/test/build commands, Admin/public route boundaries, `Cache-Control:no-store`, and filing defaults/link/gate. Assert root README links the Stage 2 plan, service guide, GFS contract, design, and roadmap and no longer says later blog-management APIs are wholly unimplemented.

- [ ] **Step 2: Run the docs test and record the RED**

```bash
cd service
GOTOOLCHAIN=go1.25.7 go test ./internal/app -run TestStage2Documentation -v
```

Expected: FAIL because Stage 2 operations are not documented.

- [ ] **Step 3: Update service and root documentation**

Add exact environment rows for `BLOG_GFS_BASE_URL`, `BLOG_GFS_APP_ID`, `BLOG_GFS_APP_SECRET`, and `BLOG_GFS_PUBLIC_READ_SECRET`, noting production HTTPS and secret handling. Explain that the raw GFS app secret is digested locally to match GFS validation and must never be logged. Document manual schema application for a disposable/development database before the new binary starts; do not instruct users to run a service migration command. Document dedicated private GFS bucket/app provisioning, metadata/302 compatibility, upload and proxy behavior, media limits, dynamic hotlink semantics, site settings defaults, fixed filing URL, and that Release enforcement begins in Stage 3 via the already-tested validator.

- [ ] **Step 4: Run formatting, generation, static analysis, unit, race, and flow gates**

Run exactly:

```bash
cd service
export GOTOOLCHAIN=go1.25.7
test "$(go env GOVERSION)" = "go1.25.7"
gofmt -w $(find . -name '*.go' -not -path './build/*')
make generate
git diff --exit-code -- internal/httpapi/admin.gen.go
go test ./...
go test -race ./internal/...
go test ./tests/flow/... -v
go vet ./...
GOARCH=amd64 make build
file build/blog-service
```

Expected: PASS; the exact toolchain assertion succeeds, generation is clean, all tests/race/flow/vet commands pass, and `file` reports a Linux x86-64 statically linked or not-dynamically-linked executable.

- [ ] **Step 5: Verify security, architecture, and scope mechanically**

Run:

```bash
cd service
rg -n 'AUTO_INCREMENT|UNSIGNED' sqls/develop/develop.sql
rg -n 'ExecContext|QueryContext|QueryRowContext|BeginTx' internal/httpapi
rg -n 'NewRedisCounter|idgen.New\(' internal
rg -n 'http\.Get|http\.Post|DefaultClient' internal/media internal/settings
rg -n 'request.body|Request\.Body|Cookie|Authorization|GFS.*Secret|signature' internal/app internal/httpapi internal/media
if [ -d sqls/releases ]; then find sqls/releases -type f -name 'v*.sql' -print; fi
git status --short
```

Expected: no unsigned/autoincrement DDL; no SQL in handlers; exactly one production Redis counter/generator construction in app plus explicit tests; no default HTTP client; no sensitive logging; no release SQL; only expected generated/build/document changes remain.

- [ ] **Step 6: Perform the final specification/type review**

Read the design sections for IDs, revisions, tags, media, settings, lifecycle, security, image access, API boundaries, filing compliance, and tests next to the implementation diff. Verify:

- every new table ID is signed BIGINT and every new repository receives the one shared generator;
- article slugs and media keys are high-entropy, URL-safe, stable, and non-enumerable;
- autosave conflicts cannot overwrite, frozen rows cannot update, tag snapshots survive rename, restore copies rather than mutates, and trash cannot hide published content;
- transient blobs cannot freeze, media references synchronize on save, and media deletion is absent;
- site settings remain pending/publishable, filing validation is reusable, and hotlink writes invalidate cache before response;
- policy path/TTL and GFS formulas match source, metadata is verified, and public redirect is temporary/no-store without byte proxying;
- Admin routes carry auth/Origin rules, public proxy does not, Problems remain sanitized, and logs contain no secrets;
- no Release/Jenkins/Astro implementation entered this stage.

Scan the plan and implementation for unfinished markers without embedding them as continuous search literals:

```bash
bad='TO''DO|TB''D|FIX''ME|XX''X|<in''sert|<fi''ll|implement la''ter'
! rg -n "$bad" ../docs/superpowers/plans/2026-08-13-service-content-media.md ../contracts . ../README.md
rg -n 'uint64|type: string.*[Ii]d|AUTO_INCREMENT|UNSIGNED' ../contracts/openapi/admin-v1.yaml internal sqls/develop/develop.sql
```

Expected: unfinished-marker scan is empty. The type scan has no Stage 2 unsigned/string entity IDs and no forbidden DDL; inspect any unrelated textual match rather than suppressing it.

- [ ] **Step 7: Commit documentation and final verification record**

```bash
git add README.md service/README.md service/internal/app/docs_test.go
git commit -m "docs: document service content media"
git status --short
git log --oneline --decorate -14
```

Expected: documentation test and the complete gate remain green; worktree is clean; commits are task-sized and ordered as this plan specifies.

## Plan Self-Review Checklist

- [ ] The plan starts with the required implementation-plan header, names the implementation sub-skill, and uses checkbox steps.
- [ ] The full design, roadmap Stage 2, current foundation plan/code/OpenAPI, and GFS metadata/redirect/signature source have been reconciled.
- [ ] The file map names every create/modify/generate target; each task states concrete consumed and produced interfaces.
- [ ] RED and GREEN commands are explicit, use Go `1.25.7`, and rely only on sqlmock, miniredis, fakes, and `httptest` for Service automation.
- [ ] DDL is manual `develop.sql` only; no release SQL, migration runtime, boot SQL, container, or deployed dependency is introduced.
- [ ] Signed BIGINT IDs, shared Redis generator injection, explicit ordering, immutable slugs/keys, and association-table IDs are all covered by tests.
- [ ] Article creation, autosave conflict, manual versions, tag snapshots, restore, preview, trash/untrash, and published-trash rejection have concrete APIs and transaction rules.
- [ ] Site settings, fixed/default filing values, reusable release gate, hotlink defaults, exact Referer semantics, atomic replacement, and immediate cache invalidation are covered.
- [ ] GFS policy signing, fixed path/TTL, actual metadata validation, allowed media limits, stable proxy URLs, local read signing, temporary redirect, no-store, and no byte proxying are covered.
- [ ] OpenAPI full Admin paths, composite handler, root public route, app/process wiring, flow proof, documentation, and final build/static/security checks are included.
- [ ] All entity IDs are consistently `int64`/OpenAPI `format:int64`; nullable identities use pointers/nullability rather than zero; lock versions and revision numbers are signed `int64`.
- [ ] The plan contains no unfinished implementation markers or unspecified decisions; deferred Release/Jenkins/Site work is explicitly outside Stage 2 rather than left ambiguous.
