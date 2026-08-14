# Service Immutable Releases and Jenkins Orchestration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add serial, immutable whole-site releases, versioned downloadable bundles, Jenkins configuration and orchestration, authenticated callbacks, retry, and safe deployed-artifact reconciliation to the Go service.

**Architecture:** Stage 3 builds two focused domains. `release` owns immutable snapshot persistence, bundle serialization, the `site_state` transactional lock, publish-job state, retry, and reconciliation; `builder` owns encrypted Jenkins configuration plus HTTPS-only test and trigger clients. HTTP handlers remain thin adapters: Admin routes require the existing session middleware and exact-Origin guard, while Internal routes independently authenticate a bundle Bearer token or a signed Jenkins callback. SQL remains a manually reviewed forward-only file; neither startup nor tests execute it against a real MySQL server.

**Tech Stack:** Go 1.25.7, Gin 1.12.0, `database/sql` + MySQL driver 1.10.0, go-redis/v9 9.22.0, `crypto/aes`/`cipher`/`hmac`/`sha256`, gzip/JSON, oapi-codegen 2.8.0, kin-openapi, sqlmock, miniredis, Testify, fakes, and httptest.

## Global Constraints

- Work on Stage 3 only after Stage 2 has delivered article/revision/tag/site-settings repositories and its Admin routes; do not reimplement Stage 2 behavior.
- Use Go exactly `1.25.7`; retain `go 1.25.0` and `toolchain go1.25.7` in `service/go.mod`.
- All database changes append only to `service/sqls/develop/develop.sql`; operators review and execute SQL manually. Do not add a migration library, migration command, Down SQL, startup DDL, or boot-time SQL-file reads.
- Every new MySQL primary key is `BIGINT NOT NULL`, every Go/API ID is signed `int64`, and new persistent IDs come from the one injected shared Redis `idgen.Generator`. Never use `UNSIGNED`, `AUTO_INCREMENT`, `LastInsertId`, random IDs, or IDs as chronological order.
- `IDGEN_HEAL` behavior stays bounded to named MySQL `PRIMARY` conflicts; business unique-key conflicts never trigger healing.
- Releases are immutable: subsequent draft/settings/tag changes must never alter release rows, `release_articles`, bundle bytes, checksum, ETag, or a retry’s source content.
- Lock `site_state` with one transaction and `SELECT ... FOR UPDATE`; only one non-final publish job may exist. Do not infer a lock from an in-memory mutex.
- A failed trigger/build/deploy/callback never advances `current_release_id`, article `published_revision_id`, or the deployed public site pointer. Retrying creates a new job for the same Release.
- Bundle schema version is integer `1`; bundle checksum is lowercase `sha256:<64 hex>` over canonical JSON containing exactly `site`, `tags`, and `articles`, with keys recursively sorted, compact separators, UTF-8, and no `checksum` member.
- Bundle retrieval requires a constant-time comparison of `Authorization: Bearer <BLOG_BUNDLE_TOKEN>`; it returns a known immutable Release unless its latest job is final `failed`, gzip when `Accept-Encoding` contains `gzip`, and `ETag` equal to the checksum on compressed and identity responses. Jenkins must be able to download the queued/building/deploying Release before it can report final success.
- `BLOG_BUNDLE_TOKEN`, `BLOG_BUILDER_MASTER_KEY`, and `BLOG_CALLBACK_HMAC_KEY` are required opaque secrets. Master key is exactly 32 bytes after base64 RawStdEncoding decoding; bundle and callback keys are 32–128 bytes. Never put any secret, plaintext Jenkins token, encrypted token, Bearer token, callback signature, or full external URL in client errors or logs.
- Encrypt Jenkins API tokens with AES-256-GCM using a fresh 12-byte nonce per encryption. Persist `nonce || ciphertext-and-tag` as base64 RawStdEncoding; decrypt only immediately before a Jenkins HTTP request.
- Jenkins base URLs must be canonical HTTPS origins without userinfo, query, fragment, path, or a trailing slash. Job names match `^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`, contain no `//`, and are URL path-segment escaped for every component.
- Jenkins test and trigger use an injected `*http.Client` with a bounded request context and never follow redirects. Trigger only `POST <base>/job/<escaped segments>/buildWithParameters` with form fields `RELEASE_ID` and `PUBLISH_JOB_ID`; use basic authentication from configured username/token, accept 201 or 302 only when redirect following is disabled, and never write credentials into error text.
- Callbacks accept JSON only, limit bodies to 16 KiB, require canonical signed `timestamp`/`nonce`/payload, reject timestamp skew over five minutes, perform constant-time HMAC-SHA256 verification, and claim nonce with Redis `SET NX EX` before state mutation. Duplicate nonce and duplicate state are successful idempotent acknowledgements with no repeated state effects.
- Callback transitions are monotonic: `queued -> building -> deploying -> success|failed`; `pending -> queued|failed`; a final job accepts only an identical final callback. Any other transition returns a generic conflict problem and leaves data unchanged.
- Reconciliation reads only configured local `current/release.json` via an injected reader. It accepts only a regular JSON file with known completed release ID, matching checksum, positive build number, and a deployment timestamp; it updates pointers only under `site_state` lock. Missing file means no reconciliation; malformed, unknown, mismatched, or contradictory data blocks new publication and is surfaced only as a generic dependency-unavailable/admin-operational error.
- Admin unsafe endpoints use existing `OriginGuard`; Admin endpoints use `RequireAdmin`; Internal endpoints never use browser cookies or CORS. All errors use the existing RFC-9457 Problem shape and request ID.
- Automated tests use sqlmock, miniredis, deterministic fakes, and httptest only. Never start Docker, Testcontainers, Jenkins, or a real network/MySQL/Redis deployment.
- Run `make generate` and keep `service/internal/httpapi/admin.gen.go` clean. Update `service/README.md`, root README only if its stated phase availability changes, and the contract tests whenever OpenAPI or Bundle Schema changes.

---

## Planned File Map

```text
contracts/openapi/admin-v1.yaml                         Admin release/builder operations and internal endpoints
contracts/release-bundle-v1.schema.json                 Versioned portable Bundle JSON contract
service/oapi-codegen.yaml                               Existing generated Gin server configuration
service/sqls/develop/develop.sql                        Manual forward-only Stage 3 DDL
service/sqls/sql_contract_test.go                       Static DDL/SQL-lifecycle assertions
service/internal/config/config.go                       Secret/key/path configuration parsing and redacted validation
service/internal/config/config_test.go                  Config defaults, parsing, and redaction tests
service/internal/platform/crypto.go                     AES-256-GCM secret box and callback HMAC helpers
service/internal/platform/crypto_test.go                Encryption/authentication/secret-redaction tests
service/internal/release/model.go                       Immutable snapshot, job, bundle, and reconciliation values/errors
service/internal/release/repository.go                  Repository and Stage 2 snapshot-source contracts
service/internal/release/repository_mysql.go            Transactional release/site-state/publish-job persistence
service/internal/release/repository_mysql_test.go       sqlmock transaction, lock, signed-ID, and immutability tests
service/internal/release/bundle.go                      Canonical Bundle assembly, checksum, gzip, and ETag helpers
service/internal/release/bundle_test.go                 Schema, canonicalization, gzip, and checksum tests
service/internal/release/service.go                     Create, retry, transition, and reconciliation orchestration
service/internal/release/service_test.go                Fakes covering locks, failures, retry, and pointer semantics
service/internal/release/reconcile.go                   release.json parser and file-reader contract
service/internal/release/reconcile_test.go              Artifact acceptance/rejection tests
service/internal/builder/model.go                       Builder configuration, test/trigger contracts and errors
service/internal/builder/repository.go                  Encrypted builder-config persistence boundary
service/internal/builder/repository_mysql.go            Single-builder MySQL adapter
service/internal/builder/repository_mysql_test.go       sqlmock encryption/no-token-readback tests
service/internal/builder/jenkins.go                     HTTPS validation and injected Jenkins HTTP client
service/internal/builder/jenkins_test.go                httptest request/redirect/auth/error-redaction tests
service/internal/builder/callback.go                    Signed callback decoding, nonce replay store, and transition adapter
service/internal/builder/callback_test.go               HMAC, window, replay, and idempotence tests
service/internal/httpapi/release_handler.go             Admin release/builder and Internal Bundle/callback handlers
service/internal/httpapi/release_handler_test.go        Problem, auth, headers, and request-decoding tests
service/internal/httpapi/internal_auth.go               Bundle Bearer and callback authentication middleware
service/internal/httpapi/internal_auth_test.go           Constant-time auth and route-isolation tests
service/internal/httpapi/contract_test.go               Expanded OpenAPI and Bundle Schema validation
service/internal/app/app.go                             Shared Generator and new release/builder dependency composition
service/internal/app/app_test.go                        Router registration and direct-build configuration tests
service/cmd/blog-service/main.go                        Inject local release.json reader and configured HTTP client
service/cmd/blog-service/main_test.go                   Startup/reconciliation failure and ownership tests
service/tests/flow/release_test.go                      In-process sqlmock/miniredis release-to-callback proof
service/README.md                                       Variables, manual SQL, Jenkins, Bundle and reconciliation operator guide
README.md                                               Stage availability summary after Stage 3 delivery
```

The Stage 2 repository consumed by this plan must provide these read-only snapshot methods; implementers must add an adapter in Stage 2’s existing package if its names differ, rather than query mutable tables from HTTP code:

```go
type SnapshotSource interface {
    LoadCurrentSite(ctx context.Context) (SiteSnapshot, error)
    LoadPublishedArticles(ctx context.Context, currentReleaseID int64) ([]ArticleSnapshot, error)
    FreezeForPublish(ctx context.Context, articleID int64) (ArticleSnapshot, error)
    RemoveFromPublish(ctx context.Context, articleID int64) error
}

type SiteSnapshot struct { Name, AuthorBio, AboutMarkdown, FilingName, FilingNumber string; SocialLinks []SocialLink }
type ArticleSnapshot struct { ArticleID, RevisionID int64; Slug, Title, Summary, ContentMarkdown, ContentHash string; PublishedAt time.Time; Tags []TagSnapshot }
type TagSnapshot struct { ID int64; Name, Slug string }
type SocialLink struct { Label, URL string }
```

### Task 1: Freeze the Release and Jenkins Contracts, Models, and Manual Schema

**Files:**
- Create: `contracts/release-bundle-v1.schema.json`
- Modify: `contracts/openapi/admin-v1.yaml`
- Modify: `service/sqls/develop/develop.sql`
- Modify: `service/sqls/sql_contract_test.go`
- Create: `service/internal/release/model.go`
- Create: `service/internal/release/repository.go`
- Create: `service/internal/release/model_test.go`
- Modify: `service/internal/httpapi/contract_test.go`

**Interfaces:**
- Consumes: Stage 2 `SnapshotSource`, existing `idgen.Generator`, existing `httpapi.Problem`.
- Produces: `release.Release`, `release.PublishJob`, `release.Bundle`, `release.Repository`, exact JSON Schema and generated route/model source contract.

- [ ] **Step 1: Write failing contract/model/DDL tests**

```go
func TestReleaseBundleSchemaRequiresImmutableChecksum(t *testing.T) {
    schema := loadJSONSchema(t, "../../../contracts/release-bundle-v1.schema.json")
    require.NoError(t, schema.Validate(map[string]any{
        "schemaVersion": 1, "releaseId": int64(7), "generatedAt": "2026-08-13T12:00:00Z",
        "site": map[string]any{"filingName":"长安休息室","filingNumber":"浙ICP备17057726号-1"},
        "tags": []any{}, "articles": []any{}, "checksum": "sha256:"+strings.Repeat("a", 64),
    }))
}

func TestDevelopSQLDefinesReleaseStateWithoutUnsignedOrAutoIncrement(t *testing.T) {
    sql := strings.ToUpper(readDevelopSQL(t))
    for _, table := range []string{"RELEASES", "RELEASE_ARTICLES", "PUBLISH_JOBS", "SITE_STATE", "BUILDER_CONFIG"} {
        require.Contains(t, sql, "CREATE TABLE "+table)
    }
    require.NotContains(t, sql, "AUTO_INCREMENT")
    require.NotContains(t, sql, "UNSIGNED")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./sqls ./internal/httpapi -run 'ReleaseBundle|ReleaseState' -v`

Expected: FAIL because the schema, release model, API operations, and DDL do not exist.

- [ ] **Step 3: Define the versioned JSON/OpenAPI contract and domain types**

Add `contracts/release-bundle-v1.schema.json` with `additionalProperties: false` throughout, positive signed integer IDs, RFC3339 date-times, checksum pattern `^sha256:[0-9a-f]{64}$`, and only the following public shape:

```json
{"schemaVersion":1,"releaseId":7,"generatedAt":"2026-08-13T12:00:00Z","site":{"name":"","authorBio":"","aboutMarkdown":"","filingName":"","filingNumber":"","socialLinks":[]},"tags":[{"id":1,"name":"go","slug":"t_abcdefghijkl"}],"articles":[{"articleId":2,"revisionId":3,"slug":"example_slug","title":"","summary":"","contentMarkdown":"","contentHash":"sha256:...","publishedAt":"2026-08-13T12:00:00Z","tags":["t_abcdefghijkl"]}],"checksum":"sha256:..."}
```

Extend OpenAPI with Admin `GET/PUT /api/admin/v1/builder`, `POST /api/admin/v1/builder/test`, `POST /api/admin/v1/releases`, `GET /api/admin/v1/releases`, `GET /api/admin/v1/releases/{releaseId}`, and `POST /api/admin/v1/releases/{releaseId}/retry`; add Internal `GET /api/internal/v1/releases/{releaseId}/bundle` and `POST /api/internal/v1/jenkins/callback`. Document 200/201/202/204 plus 400/401/403/409/412/429/503 Problem responses. The request schemas must reject extra fields and never expose a stored Jenkins token.

Create `release/model.go`:

```go
type ReleaseStatus string
const (ReleaseQueued ReleaseStatus = "queued"; ReleaseSuccess ReleaseStatus = "success"; ReleaseFailed ReleaseStatus = "failed")
type JobStatus string
const (JobPending JobStatus = "pending"; JobQueued JobStatus = "queued"; JobBuilding JobStatus = "building"; JobDeploying JobStatus = "deploying"; JobSuccess JobStatus = "success"; JobFailed JobStatus = "failed")
type Release struct { ID int64; Status ReleaseStatus; Site SiteSnapshot; Checksum string; CreatedAt time.Time; CompletedAt *time.Time }
type PublishJob struct { ID, ReleaseID, BuilderID int64; Status JobStatus; Stage string; BuildNumber *int64; ErrorSummary string; CreatedAt time.Time; FinishedAt *time.Time }
type Bundle struct { SchemaVersion int `json:"schemaVersion"`; ReleaseID int64 `json:"releaseId"`; GeneratedAt time.Time `json:"generatedAt"`; Site BundleSite `json:"site"`; Tags []BundleTag `json:"tags"`; Articles []BundleArticle `json:"articles"`; Checksum string `json:"checksum"` }
var (ErrBusy = errors.New("publish already active"); ErrNotFound = errors.New("release not found"); ErrConflict = errors.New("invalid publish transition"); ErrReconciliationRequired = errors.New("release reconciliation required"))
```

Define repository signatures in `repository.go`, including transaction-scoped methods:

```go
type Repository interface {
    CreateLocked(ctx context.Context, command CreateCommand) (Release, PublishJob, error)
    FindRelease(ctx context.Context, id int64) (Aggregate, error)
    ListReleases(ctx context.Context, query ListQuery) ([]Aggregate, error)
    LoadBundle(ctx context.Context, id int64) (Bundle, error)
    CreateRetryLocked(ctx context.Context, releaseID int64) (Aggregate, PublishJob, error)
    ApplyCallbackLocked(ctx context.Context, event CallbackEvent) (PublishJob, bool, error)
    FailTriggerLocked(ctx context.Context, publishJobID int64, summary string, at time.Time) (PublishJob, bool, error)
    ReconcileLocked(ctx context.Context, artifact Artifact) (bool, error)
}
type CreateCommand struct { Mode PublishMode; ArticleID int64; BuilderID int64; RequestedBy int64 }
type CallbackEvent struct { ReleaseID, PublishJobID, BuildNumber int64; Stage string; Status JobStatus; ErrorSummary string; Timestamp time.Time; Nonce string }
type Artifact struct { ReleaseID int64; Checksum string; BuildNumber int64; DeployedAt time.Time }
```

- [ ] **Step 4: Append forward-only manual DDL**

Append DDL for `releases`, `release_articles`, `publish_jobs`, singleton `site_state`, and singleton `builder_config`. Each entity table gets `id BIGINT NOT NULL` and a primary key. `release_articles` has `release_id`, `article_id`, `revision_id`, a unique `(release_id, article_id)`, and copies all Bundle article fields/snapshots needed to generate a Bundle without touching mutable draft tables. `releases` stores canonical `site_snapshot_json`, `checksum`, status, creation/completion timestamps. `publish_jobs` stores explicit `release_id`, `builder_id`, job state/stage/build number/error summary/timestamps. `site_state` has `singleton_key TINYINT NOT NULL DEFAULT 1`, nullable `current_release_id`, nullable `active_publish_job_id`, and a unique singleton key. `builder_config` has encrypted token text plus nonce/ciphertext column only; do not add plaintext token.

- [ ] **Step 5: Run focused tests and generate code**

Run: `cd service && GOTOOLCHAIN=go1.25.7 make generate && git diff --exit-code -- internal/httpapi/admin.gen.go && go test ./sqls ./internal/release ./internal/httpapi -run 'ReleaseBundle|ReleaseState|Contract' -v`

Expected: PASS. The generated file reflects all operations and no test contacts real infrastructure.

- [ ] **Step 6: Commit**

```bash
git add contracts/openapi/admin-v1.yaml contracts/release-bundle-v1.schema.json service/oapi-codegen.yaml service/sqls/develop/develop.sql service/sqls/sql_contract_test.go service/internal/release/model.go service/internal/release/repository.go service/internal/release/model_test.go service/internal/httpapi/admin.gen.go service/internal/httpapi/contract_test.go
git commit -m "feat(service): define immutable release contracts"
```

### Task 2: Add Secret Configuration and Cryptographic Boundaries

**Files:**
- Modify: `service/internal/config/config.go`
- Modify: `service/internal/config/config_test.go`
- Create: `service/internal/platform/crypto.go`
- Create: `service/internal/platform/crypto_test.go`

**Interfaces:**
- Consumes: existing `config.Load`/`Validate` and redacted error conventions.
- Produces: `config.ReleaseConfig`, `platform.SecretBox`, `platform.Signature`, and parsed canonical secrets for later builder/internal HTTP tasks.

- [ ] **Step 1: Write failing config and crypto tests**

```go
func TestLoadRejectsMissingOrInvalidReleaseSecretsWithoutEchoingThem(t *testing.T) {
    env := validEnv(); env["BLOG_BUNDLE_TOKEN"] = "private-token"
    delete(env, "BLOG_CALLBACK_HMAC_KEY")
    _, err := config.Load(getenv(env))
    require.ErrorContains(t, err, "BLOG_CALLBACK_HMAC_KEY")
    require.NotContains(t, err.Error(), "private-token")
}

func TestSecretBoxUsesFreshNonceAndRejectsTampering(t *testing.T) {
    box := mustSecretBox(t, bytes.Repeat([]byte{7}, 32))
    first, _ := box.Seal([]byte("jenkins-token")); second, _ := box.Seal([]byte("jenkins-token"))
    require.NotEqual(t, first, second)
    _, err := box.Open(tamper(first)); require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./internal/config ./internal/platform -run 'ReleaseSecrets|SecretBox' -v`

Expected: FAIL because release secret configuration and crypto helpers are absent.

- [ ] **Step 3: Add explicit config and AES/HMAC helpers**

Extend `Config` with:

```go
type ReleaseConfig struct { BundleToken []byte; CallbackHMACKey []byte; BuilderMasterKey []byte; CurrentReleaseJSONPath string }
```

Read `BLOG_BUNDLE_TOKEN`, `BLOG_CALLBACK_HMAC_KEY`, `BLOG_BUILDER_MASTER_KEY`, and `BLOG_CURRENT_RELEASE_JSON_PATH`. Require nonblank Bundle/callback values of 32–128 bytes, require a RawStdEncoding-decoded 32-byte master key, and default the artifact path to `/web/deploy/blog-site/current/release.json`. Reject blank/whitespace path but do not require the file during config loading. All errors name the variable only.

Implement:

```go
type SecretBox struct { key [32]byte; random io.Reader }
func NewSecretBox(key []byte, random io.Reader) (SecretBox, error)
func (b SecretBox) Seal(plaintext []byte) (string, error)
func (b SecretBox) Open(encoded string) ([]byte, error)
func ComputeHMAC(key, canonical []byte) string
func VerifyHMAC(key, canonical []byte, provided string) bool
```

Use `aes.NewCipher`, `cipher.NewGCM`, `io.ReadFull` for a nonce, `gcm.Seal(nonce, nonce, plaintext, nil)`, RawStdEncoding, strict canonical decoding, and `hmac.Equal`. Every failure returns a static message without inputs.

- [ ] **Step 4: Run focused tests**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./internal/config ./internal/platform -run 'ReleaseSecrets|SecretBox|HMAC' -v`

Expected: PASS, including malformed base64, wrong key size, nil randomness, altered ciphertext, noncanonical encodings, and secret redaction.

- [ ] **Step 5: Commit**

```bash
git add service/internal/config/config.go service/internal/config/config_test.go service/internal/platform/crypto.go service/internal/platform/crypto_test.go
git commit -m "feat(service): secure release credentials"
```

### Task 3: Persist Immutable Releases, Publish Jobs, and the Database Lock

**Files:**
- Create: `service/internal/release/repository_mysql.go`
- Create: `service/internal/release/repository_mysql_test.go`
- Modify: `service/internal/release/repository.go`

**Interfaces:**
- Consumes: Task 1 release values, Stage 2 `SnapshotSource`, one process-shared `*idgen.Generator`.
- Produces: `release.NewMySQLRepository(db *sql.DB, ids *idgen.Generator, snapshots SnapshotSource) *MySQLRepository` implementing all `Repository` methods.

- [ ] **Step 1: Write failing sqlmock tests for creation and lock semantics**

```go
func TestCreateLockedLocksSiteStateAndSnapshotsBeforeCreatingSignedRows(t *testing.T) {
    repo, mock, source := newReleaseRepositoryTest(t, rawIDs(1, 2))
    mock.ExpectBegin()
    mock.ExpectQuery(regexp.QuoteMeta("SELECT current_release_id, active_publish_job_id FROM site_state WHERE singleton_key = ? FOR UPDATE")).WithArgs(1).
        WillReturnRows(siteStateRows(nil, nil))
    source.expectFreeze(41, immutableArticle(41, 71))
    mock.ExpectExec(insertReleaseSQL).WithArgs(int64(1), sqlmock.AnyArg(), sqlmock.AnyArg(), "queued").WillReturnResult(sqlmock.NewResult(0, 1))
    mock.ExpectExec(insertReleaseArticleSQL).WithArgs(int64(1), int64(41), int64(71), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
    mock.ExpectExec(insertPublishJobSQL).WithArgs(int64(2), int64(1), int64(9), "pending").WillReturnResult(sqlmock.NewResult(0, 1))
    mock.ExpectExec(updateActiveJobSQL).WithArgs(int64(2), 1).WillReturnResult(sqlmock.NewResult(0, 1))
    mock.ExpectCommit()
    _, job, err := repo.CreateLocked(context.Background(), CreateCommand{Mode: PublishArticle, ArticleID: 41, BuilderID: 9})
    require.NoError(t, err); require.Equal(t, int64(2), job.ID)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./internal/release -run 'CreateLocked|RetryLocked|ApplyCallbackLocked' -v`

Expected: FAIL because the MySQL repository does not exist.

- [ ] **Step 3: Implement transaction-scoped persistence**

`CreateLocked` must begin a transaction, lock exactly the `site_state` singleton row, reject non-null active job with `ErrBusy`, ask `SnapshotSource` for frozen/current snapshots according to explicit `PublishMode` (`PublishArticle`, `UnpublishArticle`, `PublishSettings`), generate signed Release/Job IDs through the injected generator before INSERT, serialize copied snapshot fields, create `release_articles`, set `active_publish_job_id`, and commit. For every mode, read the current Release snapshot as the base; never query mutable drafts except `FreezeForPublish` for the selected article.

`CreateRetryLocked` locks `site_state`, rejects active work, verifies the Release exists and is immutable/failed or previously attempted, inserts a fresh pending job, and updates the active pointer without changing Release rows. `ApplyCallbackLocked` locks the exact `(PublishJobID, ReleaseID)` job row, requires `site_state.active_publish_job_id` to equal that job for non-final mutation, performs the exact monotonic transition, assigns a positive build number once, records only a bounded 512-rune sanitized summary, clears the active lock on final outcome, and updates `current_release_id` plus Stage 2 published-revision pointers only for success. Existing matching state for that same job returns `(job, true, nil)`; a delayed callback for an older job cannot mutate a retry. `FailTriggerLocked` locks the exact active job by ID and records an idempotent buildless trigger failure without fabricating a Jenkins callback. Invalid identity or order returns `ErrConflict`.

- [ ] **Step 4: Add failure/immutability test cases**

Add sqlmock coverage for active-lock rejection with rollback, snapshot-source failure rollback, ID-generator failure before INSERT, `PRIMARY` healing propagation, named business unique error without healing, success pointer update, failed callback clearing only active job, duplicate callback no duplicate update, invalid transition rollback, retry retaining exact checksum/release rows, and SQL parameters never using unsigned values or `LastInsertId`.

- [ ] **Step 5: Run repository tests**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./internal/release -run MySQLRepository -v`

Expected: PASS with all transaction boundaries and expectations met.

- [ ] **Step 6: Commit**

```bash
git add service/internal/release/repository.go service/internal/release/repository_mysql.go service/internal/release/repository_mysql_test.go
git commit -m "feat(service): persist immutable release snapshots"
```

### Task 4: Build Canonical Bundles and Reconcile Deployed release.json

**Files:**
- Create: `service/internal/release/bundle.go`
- Create: `service/internal/release/bundle_test.go`
- Create: `service/internal/release/reconcile.go`
- Create: `service/internal/release/reconcile_test.go`
- Modify: `service/internal/release/service.go`
- Create: `service/internal/release/service_test.go`

**Interfaces:**
- Consumes: Task 3 `Repository`, `contracts/release-bundle-v1.schema.json`.
- Produces: `Service.Bundle`, `Service.Create`, `Service.Retry`, `Service.ApplyCallback`, `Service.Reconcile`, `ReadArtifact`.

- [ ] **Step 1: Write failing deterministic Bundle and reconciliation tests**

```go
func TestBundleBytesStayIdenticalAfterMutableSourceChanges(t *testing.T) {
    service := newReleaseService(t, immutableRepositoryFixture())
    first, etag, err := service.Bundle(context.Background(), 7)
    mutateStageTwoDraftAndSettings()
    second, secondETag, err := service.Bundle(context.Background(), 7)
    require.NoError(t, err); require.Equal(t, first, second); require.Equal(t, etag, secondETag)
}

func TestReadArtifactRejectsChecksumMismatchAndAcceptsKnownRelease(t *testing.T) {
    artifact, err := ReadArtifact(strings.NewReader(`{"releaseId":7,"checksum":"sha256:`+strings.Repeat("a",64)+`","buildNumber":12,"deployedAt":"2026-08-13T12:00:00Z"}`))
    require.NoError(t, err); require.Equal(t, int64(7), artifact.ReleaseID)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./internal/release -run 'Bundle|Artifact|Reconcile' -v`

Expected: FAIL because canonical serialization and artifact reconciliation are absent.

- [ ] **Step 3: Implement canonical assembly, checksum, gzip, and artifact parser**

Use dedicated `BundleSite`, `BundleTag`, and `BundleArticle` values with deterministic sort order: tags by slug then ID; articles by published timestamp then article ID; each article tag list by slug then ID. Marshal a separate checksum payload struct without `Checksum` through a recursive canonical JSON encoder that sorts object keys. Hash those bytes, assign `sha256:<hex>`, then marshal the complete Bundle canonically. `Bundle` returns identity bytes and checksum ETag; the HTTP layer compresses with `gzip.NewWriter` only after obtaining those immutable bytes.

Implement:

```go
type ArtifactReader func() (io.ReadCloser, error)
func ReadArtifact(r io.Reader) (Artifact, error)
func (s Service) Reconcile(ctx context.Context, read ArtifactReader) (bool, error)
```

Decode one strict JSON object, reject unknown/duplicate fields, trailing data, nonpositive IDs/build number, invalid checksum, zero timestamp, and oversized 4 KiB files. `Reconcile` treats `fs.ErrNotExist` as `(false,nil)`; otherwise passes a validated Artifact to `Repository.ReconcileLocked`. It never marks a job/release successful based on an artifact that differs from stored checksum or known Release.

- [ ] **Step 4: Add complete Bundle/reconciliation edge coverage**

Test schema validation of produced JSON, identity and gzip decompression equality, ETag equality to checksum, no secret/draft/hash leakage beyond defined bundle data, changing maps/input iteration cannot change bytes, release-not-found/final-failure response while queued/building/deploying downloads remain available, current artifact missing, malformed JSON, unknown release, altered checksum/build number, and safe reconciliation updating pointers exactly once.

- [ ] **Step 5: Run focused tests**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./internal/release -run 'Bundle|Artifact|Reconcile|ReleaseService' -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add service/internal/release/bundle.go service/internal/release/bundle_test.go service/internal/release/reconcile.go service/internal/release/reconcile_test.go service/internal/release/service.go service/internal/release/service_test.go
git commit -m "feat(service): assemble immutable release bundles"
```

### Task 5: Encrypt Builder Configuration and Call Jenkins Safely

**Files:**
- Create: `service/internal/builder/model.go`
- Create: `service/internal/builder/repository.go`
- Create: `service/internal/builder/repository_mysql.go`
- Create: `service/internal/builder/repository_mysql_test.go`
- Create: `service/internal/builder/jenkins.go`
- Create: `service/internal/builder/jenkins_test.go`

**Interfaces:**
- Consumes: Task 2 `platform.SecretBox`, configured HTTP client.
- Produces: `builder.ConfigRepository`, `builder.ValidateConfig`, `builder.Client.Test`, `builder.Client.Trigger`.

- [ ] **Step 1: Write failing encryption, URL, and HTTP tests**

```go
func TestBuilderRepositoryEncryptsTokenAndNeverReturnsItToView(t *testing.T) {
    repo, mock := newBuilderRepo(t); input := builder.ConfigInput{BaseURL:"https://jenkins.example.com", Username:"ci", Token:"private-token", JobName:"site/build", Enabled:true}
    mock.ExpectExec(insertBuilderSQL).WithArgs(sqlmock.AnyArg(), "https://jenkins.example.com", "ci", sqlmock.AnyArg(), "site/build", true).WillReturnResult(sqlmock.NewResult(0,1))
    view, err := repo.Save(context.Background(), input)
    require.NoError(t, err); require.Empty(t, view.Token)
}

func TestTriggerEscapesEachJobSegmentAndDoesNotFollowRedirect(t *testing.T) {
    server, observed := newJenkinsServer(t, http.StatusFound)
    err := clientFor(server.URL).Trigger(context.Background(), config, 9, 12)
    require.NoError(t, err); require.Equal(t, "/job/site/job/build/buildWithParameters", observed.Path)
    require.Equal(t, url.Values{"RELEASE_ID":{"9"}, "PUBLISH_JOB_ID":{"12"}}, observed.Form)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./internal/builder -run 'BuilderRepository|ValidateConfig|Trigger|JenkinsTest' -v`

Expected: FAIL because builder domain and client do not exist.

- [ ] **Step 3: Implement config persistence and client**

Define:

```go
type ConfigInput struct { BaseURL, Username, Token, JobName string; Enabled bool }
type ConfigView struct { ID int64; BaseURL, Username, JobName string; Enabled, TokenConfigured bool }
type StoredConfig struct { ConfigView; EncryptedToken string }
type ConfigRepository interface { Save(ctx context.Context, input ConfigInput) (ConfigView, error); Load(ctx context.Context) (StoredConfig, error) }
type Client struct { httpClient *http.Client; timeout time.Duration }
func ValidateConfig(input ConfigInput) error
func (c Client) Test(ctx context.Context, cfg StoredConfig, box platform.SecretBox) error
func (c Client) Trigger(ctx context.Context, cfg StoredConfig, box platform.SecretBox, releaseID, publishJobID int64) (int64, error)
```

Validate and canonicalize base URL as described in Global Constraints. Store only `SecretBox.Seal([]byte(Token))`; `Save` accepts empty Token only when replacing non-secret fields on a previously configured row, and never selects/decrypts a token for an Admin response. Build a no-redirect HTTP client clone (`CheckRedirect` returns `http.ErrUseLastResponse`); both methods use `context.WithTimeout(ctx, 10*time.Second)`, decrypt in local memory, set BasicAuth, and return static operation errors. `Test` performs `GET <base>/api/json`; accepts only 200. `Trigger` requires positive Release and Publish Job IDs and sends exactly form-encoded `RELEASE_ID=<decimal signed id>&PUBLISH_JOB_ID=<decimal signed id>`, accepts 201/302, parses positive `X-Jenkins-Queue-Id` if present or returns zero queue/build number until callback supplies a build number.

- [ ] **Step 4: Add safety tests**

Cover HTTP base URL rejection (HTTP, userinfo, query, fragment, path, slash), unsafe/malformed job names, token encryption variation, ciphertext corruption, empty update preserving stored ciphertext, no token in ConfigView/errors/log strings, disabled builder rejection, auth header only at httptest origin, form payload containing only the exact Release and Publish Job IDs, 401/500/redirect/network errors, and cancellation deadline.

- [ ] **Step 5: Run focused tests**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./internal/builder -v`

Expected: PASS with no real Jenkins request.

- [ ] **Step 6: Commit**

```bash
git add service/internal/builder
git commit -m "feat(service): configure encrypted Jenkins builder"
```

### Task 6: Authenticate Callbacks, Enforce Replay Protection, and Orchestrate Publish/Retry

**Files:**
- Create: `service/internal/builder/callback.go`
- Create: `service/internal/builder/callback_test.go`
- Modify: `service/internal/release/service.go`
- Modify: `service/internal/release/service_test.go`

**Interfaces:**
- Consumes: Task 2 HMAC helpers, Task 3 repository transitions, Task 5 Jenkins Client/config repository, Redis client.
- Produces: `builder.CallbackVerifier`, `release.Orchestrator.Publish`, `release.Orchestrator.Retry`, and a callback event ready for HTTP handlers.

- [ ] **Step 1: Write failing replay/idempotence and failed-trigger tests**

```go
func TestCallbackVerifierClaimsNonceBeforeMutationAndAcceptsDuplicateIdempotently(t *testing.T) {
    redis := miniredis.RunT(t); verifier := newVerifier(t, redis)
    body := signedCallbackBody(t, verifier.Key, callback("building", "queued", "nonce-1"))
    first, duplicate, err := verifier.VerifyAndClaim(context.Background(), body)
    require.NoError(t, err); require.False(t, duplicate); require.Equal(t, "building", first.Stage)
    _, duplicate, err = verifier.VerifyAndClaim(context.Background(), body)
    require.NoError(t, err); require.True(t, duplicate)
}

func TestPublishMarksTriggerFailureFailedAndReleasesLock(t *testing.T) {
    repo := &releaseFake{created: pendingJob(2, 1), triggerErr: errors.New("private Jenkins failure")}
    _, err := orchestrator(repo).Publish(context.Background(), release.CreateCommand{Mode: release.PublishSettings, BuilderID: 8})
    require.ErrorIs(t, err, release.ErrDependencyUnavailable)
    require.Equal(t, []int64{2}, repo.failedTriggerJobIDs)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./internal/builder ./internal/release -run 'Callback|Publish|Retry|Transition' -v`

Expected: FAIL because signed callbacks and trigger orchestration are missing.

- [ ] **Step 3: Implement canonical callback verification and orchestration**

Define callback body exactly as:

```go
type CallbackPayload struct { ReleaseID int64 `json:"releaseId"`; PublishJobID int64 `json:"publishJobId"`; BuildNumber int64 `json:"buildNumber"`; Stage string `json:"stage"`; Status release.JobStatus `json:"status"`; ErrorSummary string `json:"errorSummary"`; Timestamp time.Time `json:"timestamp"`; Nonce string `json:"nonce"` }
type CallbackVerifier struct { key []byte; redis *redis.Client; now func() time.Time }
func (v CallbackVerifier) VerifyAndClaim(ctx context.Context, raw []byte, signature string) (CallbackPayload, bool, error)
```

Require a single `X-Jenkins-Signature: sha256=<lowercase hex>` header. Canonical signing bytes are `strconv.FormatInt(timestamp.Unix(),10)+"\n"+nonce+"\n"+raw`, where `raw` is the exact request body after strict JSON decode/validation and re-encode equality check and therefore includes `publishJobId`. Nonce matches `^[A-Za-z0-9_-]{16,128}$`; claim `qiuxs-blog:jenkins:nonce:<sha256(nonce)>` with five-minute TTL via `SetNX`. Validate positive Release, Publish Job, and build numbers plus stage/status pairs before Redis.

`Orchestrator.Publish` calls `release.Service.Reconcile` before `Repository.CreateLocked`, then triggers Jenkins with both `release.ID` and `job.ID` only after the creation transaction commits. On trigger error it calls `Repository.FailTriggerLocked(ctx, job.ID, summary, at)` to release that exact attempt's lock while returning `ErrDependencyUnavailable`. `Retry` calls `CreateRetryLocked`, triggers Jenkins with the unchanged Release ID and new Job ID, and calls `FailTriggerLocked` for that new Job ID on trigger failure; it never mutates the old job or Release. Callback duplicate claims acknowledge without repository changes; first claims pass both payload IDs to `ApplyCallbackLocked` exactly once. No timeout ever infers success.

- [ ] **Step 4: Add full callback/orchestration failure coverage**

Test wrong/missing/multiple signature, invalid JSON/content size/unknown fields, altered raw body, timestamp ±5 minutes and boundary, invalid nonce, Redis unavailable, repeated nonce with altered payload, valid repeated same state with different nonce, all allowed and disallowed transitions, success pointer advancement only once, failed transition retaining previous current release/published revisions, retry fresh job/reused checksum, reconciliation-required publication rejection, and no token/signature/error-summary secret reaches returned errors.

- [ ] **Step 5: Run focused tests**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./internal/builder ./internal/release -run 'Callback|Publish|Retry|Transition' -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add service/internal/builder/callback.go service/internal/builder/callback_test.go service/internal/release/service.go service/internal/release/service_test.go
git commit -m "feat(service): orchestrate Jenkins release jobs"
```

### Task 7: Expose Protected Admin and Internal HTTP APIs and Wire the Application

**Files:**
- Create: `service/internal/httpapi/internal_auth.go`
- Create: `service/internal/httpapi/internal_auth_test.go`
- Create: `service/internal/httpapi/release_handler.go`
- Create: `service/internal/httpapi/release_handler_test.go`
- Modify: `service/internal/httpapi/problem.go`
- Modify: `service/internal/app/app.go`
- Modify: `service/internal/app/app_test.go`
- Modify: `service/cmd/blog-service/main.go`
- Modify: `service/cmd/blog-service/main_test.go`

**Interfaces:**
- Consumes: generated Task 1 routes; Tasks 2–6 config, Service/Orchestrator, verifier, and client.
- Produces: complete router registration and process-owned runtime dependencies for Stage 3.

- [ ] **Step 1: Write failing HTTP/app tests**

```go
func TestBundleRequiresBearerAndReturnsGzipWithChecksumETag(t *testing.T) {
    router := releaseRouter(t, fixedBundle())
    denied := serve(router, http.MethodGet, "/api/internal/v1/releases/7/bundle", nil)
    requireProblem(t, denied, http.StatusUnauthorized, "internal_unauthorized")
    ok := serve(router, http.MethodGet, "/api/internal/v1/releases/7/bundle", map[string]string{"Authorization":"Bearer "+testBundleToken,"Accept-Encoding":"gzip"})
    require.Equal(t, http.StatusOK, ok.Code); require.Equal(t, `"sha256:`+strings.Repeat("a",64)+`"`, ok.Header().Get("ETag"))
    require.Equal(t, "gzip", ok.Header().Get("Content-Encoding"))
}

func TestAdminCreateReleaseRequiresOriginAndCurrentAdmin(t *testing.T) {
    router := releaseRouter(t, fixedBundle())
    response := serveJSON(router, http.MethodPost, "/api/admin/v1/releases", "", nil, `{}`)
    requireProblem(t, response, http.StatusForbidden, "origin_forbidden")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./internal/httpapi ./internal/app ./cmd/blog-service -run 'Bundle|Callback|CreateRelease|BuildRegisters|Reconcile' -v`

Expected: FAIL because Stage 3 handlers/middleware and wiring are absent.

- [ ] **Step 3: Implement handlers and isolated middleware**

Create constant-time Bundle Bearer middleware requiring exactly one Authorization header with `Bearer ` prefix and one exact token. It must never accept a cookie. Create callback middleware only for the callback route; it checks one `Content-Type: application/json` header, reads at most 16 KiB once, and supplies the verifier’s payload to the handler.

`release_handler.go` implements generated methods. Admin getters/listing return views with no encrypted/Jenkins secrets. Admin save-builder accepts token only in write request and produces `ConfigView`; test/create/retry map lock conflict to 409, reconciliation required to 412, dependency failures to 503. Internal Bundle emits `Content-Type: application/json`, `Vary: Accept-Encoding`, quoted ETag, `Cache-Control: no-store`, and gzip only when negotiated. Callback returns 204 for both first and duplicate accepted deliveries. Add `ErrInternalUnauthorized`/`ErrPreconditionFailed` mappings to `problem.go` with stable generic titles.

Update `app.Dependencies` with explicit injected fields rather than package globals:

```go
ReleaseJSONReader release.ArtifactReader
JenkinsHTTPClient *http.Client
```

Build exactly one shared ID generator, inject it into the existing Admin repository and new release repository; compose builder repo/box/client, release service/orchestrator, internal routes, and existing Admin middleware. Call reconciliation once in `main.run` after dependencies open and before `Build`; a malformed/mismatched artifact stops startup nonzero, absent artifact continues. Do not read the filesystem during `app.Build`.

- [ ] **Step 4: Add route/failure tests**

Cover generated OpenAPI registration, no duplicate prefixes, Admin Origin/session enforcement, Bearer absent/wrong/multiple/malformed success, cookie cannot authorize Bundle, gzip/identity same ETag/body/checksum, no-store headers, 404 unknown Release, callback handling exactly once, Problem schema validity, app direct config secret requirements, startup closes opened resources on reconcile/build failure, and no SQL runs during Build or startup.

- [ ] **Step 5: Generate and run focused tests**

Run: `cd service && GOTOOLCHAIN=go1.25.7 make generate && git diff --exit-code -- internal/httpapi/admin.gen.go && go test ./internal/httpapi ./internal/app ./cmd/blog-service -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add service/internal/httpapi service/internal/app/app.go service/internal/app/app_test.go service/cmd/blog-service/main.go service/cmd/blog-service/main_test.go service/internal/config/config.go service/internal/config/config_test.go
git commit -m "feat(service): expose release orchestration APIs"
```

### Task 8: Prove the In-Process Release Flow and Document Operations

**Files:**
- Create: `service/tests/flow/release_test.go`
- Modify: `service/README.md`
- Modify: `README.md`

**Interfaces:**
- Consumes: fully composed `app.Build`, sqlmock, miniredis, httptest, release bundle schema, and documented manual-SQL lifecycle.
- Produces: phase-level behavioral proof and operator instructions.

- [ ] **Step 1: Write the failing end-to-end in-process flow test**

```go
func TestImmutableReleaseThroughJenkinsCallbackAndRetry(t *testing.T) {
    system := newReleaseFlow(t) // sqlmock + miniredis + httptest only
    created := system.createReleaseAsAdmin(PublishArticle, 41)
    first := system.downloadBundle(created.ReleaseID)
    system.mutateDraftAndSettingsOutsideRelease()
    require.Equal(t, first.IdentityBody, system.downloadBundle(created.ReleaseID).IdentityBody)
    system.triggerCallback(created, "building", "queued", 18, "nonce-building")
    system.triggerCallback(created, "deploying", "deploying", 18, "nonce-deploying")
    system.triggerCallback(created, "failed", "failed", 18, "nonce-failed")
    require.Equal(t, system.previousCurrentRelease(), system.currentRelease())
    retried := system.retryAsAdmin(created.ReleaseID)
    require.NotEqual(t, created.JobID, retried.JobID)
    require.Equal(t, first.ETag, system.downloadBundle(created.ReleaseID).ETag)
}
```

- [ ] **Step 2: Run the flow test to verify it fails**

Run: `cd service && GOTOOLCHAIN=go1.25.7 go test ./tests/flow -run ImmutableRelease -v`

Expected: FAIL because the complete release flow is not yet wired/documented.

- [ ] **Step 3: Implement only test fixtures and documentation needed by the completed code**

Use sqlmock expectations for `site_state FOR UPDATE`, immutable INSERT/read rows, current pointer update only after success, and no pointer update on failed trigger/callback. Use miniredis for callback nonce replay and existing ID generator. Use `httptest.Server` for exact Bundle gzip/identity requests, signed callbacks, Admin Origin/session requirements, and stubbed Jenkins responses through injected transport; do not bind fixed ports or contact external services.

Document every new environment variable, base64 key requirements, filesystem artifact-path behavior, manual SQL review/execution order, Builder configuration workflow, Bundle endpoint/auth headers for Jenkins credentials, callback headers/body signature construction, accepted stages, retry behavior, and the rule that `release.json` mismatch blocks new publication and needs operator investigation. State explicitly that service never deploys files itself and Stage 6 owns Jenkins/Nginx pipelines/SSH.

- [ ] **Step 4: Run the complete verification gate**

Run:

```bash
cd service
export GOTOOLCHAIN=go1.25.7
test "$(go env GOVERSION)" = "go1.25.7"
make generate
git diff --exit-code -- internal/httpapi/admin.gen.go
go test ./...
go test -race ./internal/...
go test ./tests/flow/... -v
go vet ./...
gofmt -d $(find . -name '*.go' -not -path './build/*')
git diff --check
GOARCH=amd64 make build
file build/blog-service
```

Expected: all tests, race tests, vet, generated diff, whitespace check, and static Linux amd64 build PASS. The flow proves immutable Bundle bytes after later draft changes, callback replay protection/idempotence, failed release pointer preservation, and retry as a new job.

- [ ] **Step 5: Commit**

```bash
git add service/tests/flow/release_test.go service/README.md README.md
git commit -m "test(service): prove immutable release orchestration"
```

## Plan Self-Review

- [ ] **Coverage check:** Tasks 1–4 cover immutable snapshot rows, signed Redis IDs, manual SQL, `site_state` locking, Bundle Schema/checksum/gzip/ETag, and `release.json` reconciliation. Tasks 2 and 5 cover AES-GCM token storage, key configuration, HTTPS-only Jenkins validation/test/trigger. Task 6 covers publish/retry, HMAC/time-window/nonce/idempotent callbacks, and failed-release semantics. Tasks 7–8 cover OpenAPI/app/startup/HTTP/flow/docs integration and the approved fake-only test boundary.
- [ ] **Immutability check:** Every Bundle comes from release snapshot rows; retry creates a new job only; failure cannot advance current/published pointers; reconciliation validates known checksum before any pointer update.
- [ ] **Security check:** secrets/token/signatures are validated, constant-time compared or authenticated, never returned/logged, and all unauthenticated internal routes are explicitly isolated from cookies/CORS.
- [ ] **Placeholder check:** Search this document for unfinished-work markers, vague deferred work, and generic error-handling language; there must be no matches.
- [ ] **Type consistency check:** Verify generated operation names, `ReleaseID`/`PublishJob` signed `int64` fields, `CallbackPayload`, `Artifact`, `SecretBox`, and repository signatures match exactly across Tasks 1–8 before implementation starts.

## Final Plan Commit

```bash
git add docs/superpowers/plans/2026-08-13-service-release-jenkins.md
git commit -m "docs: plan service releases and jenkins"
git rev-parse HEAD
```
