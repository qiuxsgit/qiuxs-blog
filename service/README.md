# qiuxs-blog service

This directory contains the Go 1.25.7 Blog Service through roadmap Stage 2. It
provides health checks, administrator bootstrap and sessions, article drafts and
revision history, tags, media registration, site and hotlink settings, and the
public media redirect. Release/Jenkins orchestration belongs to Stage 3 and is
not part of this service stage.

## Configuration

All runtime configuration is supplied through environment variables.

| Variable | Required/default | Validation and purpose |
| --- | --- | --- |
| `BLOG_ENV` | `development` | Must be `development` or `production`. Production requires an HTTPS admin origin and enables Secure session cookies. |
| `BLOG_HTTP_ADDR` | `:8080` | Address passed to the HTTP server. |
| `BLOG_MYSQL_DSN` | required | MySQL DSN. Include options required by the deployment, such as `parseTime=true&loc=UTC`. |
| `BLOG_REDIS_ADDR` | required | Redis address in `host:port` form. |
| `BLOG_REDIS_PASSWORD` | empty | Redis password. |
| `BLOG_REDIS_DB` | `0` | Non-negative Redis database number. |
| `IDGEN_OFFSET` | `1` | Positive signed 64-bit lane offset. Must satisfy `1 <= offset <= step`. |
| `IDGEN_STEP` | `1` | Positive signed 64-bit lane step. Must satisfy `1 <= offset <= step`. |
| `IDGEN_HEAL` | `false` | Boolean. When enabled, a primary-key collision raises the Redis sequence from MySQL and retries; it does not run DDL. |
| `BLOG_ADMIN_ORIGIN` | required | Exact `http` or `https` origin allowed on unsafe admin requests. It must contain no userinfo, query, fragment, or non-root path; production requires HTTPS. |
| `BLOG_SESSION_COOKIE_NAME` | `qx_blog_session` | Valid ASCII HTTP cookie-token name. |
| `BLOG_SESSION_TTL` | `24h` | Go duration from `15m` through `168h` (7 days). |
| `BLOG_ADMIN_PASSWORD` | unset | Optional non-interactive input for `blog-admin init`. Prefer the hidden, twice-entered prompt so the password is not inherited or exposed through process configuration. |
| `BLOG_GFS_BASE_URL` | required | GFS `http` or `https` origin with no non-root path, query, fragment, or userinfo. An optional root `/` is normalized away; production requires HTTPS. |
| `BLOG_GFS_APP_ID` | required | Application ID for the dedicated Blog Service GFS application. |
| `BLOG_GFS_APP_SECRET` | required | Raw GFS application secret used only for local upload signing. Keep it out of source control, process output, and logs. |
| `BLOG_GFS_PUBLIC_READ_SECRET` | required | Secret used only to sign short-lived GFS read URLs locally. Keep it out of source control, process output, and logs. |

Every entity ID is allocated from a Redis counter before its MySQL `INSERT`.
IDs are positive signed `BIGINT` values and never encode timestamps or other
business data. `IDGEN_OFFSET` and `IDGEN_STEP` reserve deterministic lanes; the
default single lane produces `1, 2, 3, ...`.

## Manual SQL lifecycle

The binaries never read or execute files under `sqls/`. There is no service
migration command. There is no automatic migration at startup.

Stage 2 supplies cumulative development DDL only. Prepare a disposable or empty
development database in this order:

1. Review `sqls/develop/develop.sql` and the target database.
2. Execute `sqls/develop/develop.sql` manually from top to bottom, preserving
   its statement order.
3. Confirm the schema completed successfully before starting `blog-service`.
4. Run `blog-admin init` once to create the first administrator.

Do not replay the cumulative development file over an arbitrary populated
schema, and do not treat it as a versioned release migration. Stage 2 creates no
`v*.sql` release file. See [the SQL lifecycle notes](sqls/README.md).

## Initialize the first administrator

Prepare MySQL manually, start Redis, export the configuration above, then run:

```sh
go run ./cmd/blog-admin init --username qiuxs
```

The command prompts for the password twice without echoing it. It creates only
the first administrator; the MySQL singleton constraint remains authoritative
if initializers race.

## Provision GFS and private media storage

Provision a dedicated private GFS bucket and application for the blog. The OSS
bucket must remain private: neither the Blog Service nor the Admin persists a
public OSS object URL. The deployed GFS revision must contain both compatibility
commits:

- `f9b82569ffc5fca078053fd3fe048517fa61ab77` for actual-object metadata.
- `bcf87257e7425a6397c079aa9c9994eccbbf3aaa` for a temporary final OSS
  redirect.

The Blog Service hashes the raw GFS application secret to the MD5 digest GFS
expects and computes both upload and read signatures locally. The raw GFS
application secret, its MD5 digest, the public-read secret, policies, nonces,
signatures, signed URLs, metadata bodies, passwords, cookies, and request bodies
must never be logged.

The Admin requests an upload policy from the Blog Service, then sends the file
directly to GFS. The policy lasts exactly 60 seconds and contains only this
server-controlled path:

```text
blog/{{year}}/{{month}}/{{uuid}}.{{fileExt}}
```

No Admin request can override the path. Registration accepts a GFS file ID and
original basename, then the service verifies the actual object through
`GET /alioss/objects/{fileId}/metadata`. The metadata client uses the injected
five-second HTTP client, refuses redirects, requires a direct 200 response, and
bounds the response to 64 KiB.

Accepted images and limits are:

- `image/jpeg` with `.jpg` or `.jpeg`, `image/png` with `.png`, `image/webp`
  with `.webp`, and `image/gif` with `.gif`.
- A positive file size no greater than 10 MiB.
- Positive width and height, each no greater than 12000 pixels.
- A basename of at most 255 bytes whose extension matches its MIME type.

Successful registration returns a stable random `/img/proxy/{publicKey}` URL.
The service never exposes the sequential GFS ID in Markdown.

See the complete [GFS media contract](../docs/contracts/gfs-blog-media.md),
including multipart fields and the exact upload/read formulas. Any GFS revision
that changes the endpoint, envelope, signature, or redirect behavior requires a
coordinated compatibility review.

## HTTP boundaries and content behavior

All management operations use the `/api/admin/v1` prefix. `GET /api/admin/v1/me`
and every Stage 2 content/settings operation require the configured session
cookie from `BLOG_SESSION_COOKIE_NAME` (default `qx_blog_session`). Every unsafe
Admin request, including login and idempotent logout, must carry the exact
configured `Origin`; Stage 2 unsafe routes also require the authenticated
administrator. JSON parsing is strict and bounded, and failures use sanitized
Problem responses with request IDs.

The Admin API covers:

- Session login/logout and the current administrator.
- Article create/list/detail, optimistic draft save, preview, immutable version
  create/list/restore, and unpublished trash/untrash.
- Tag create/list/rename with historic revision snapshots.
- Media upload-policy issuance and verified registration.
- Site and Referer hotlink settings reads and writes.

Article slugs are stable 12-character lowercase URL-safe random values. Media
public keys are stable `m_` values followed by 22 lowercase URL-safe random
characters; neither exposes the underlying signed `BIGINT` ID. A draft permits
a 200-rune title, 600-rune summary, 32 tags, and 256 unique body-media references
plus one optional cover. Tag display names permit 64 runes. Raw HTML is
rejected, and a draft containing a transient `blob:` link or image cannot be
frozen into an immutable version.

The domain validator caps each raw `contentMd` and `aboutMd` field at 2 MiB.
Separately, the Admin HTTP boundary caps the entire draft-save or site-settings
JSON request body at 2 MiB, including the JSON envelope, other fields, and
string escaping. Clients must keep Markdown below the field limit so the encoded
request remains within the total-body limit. Other Stage 2 content/settings JSON
request bodies are capped at 64 KiB total. Login JSON has its own 16 KiB total
cap.

The complete Stage 2 route set is:

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/admin/v1/session` | Log in. |
| `DELETE` | `/api/admin/v1/session` | Log out idempotently. |
| `GET` | `/api/admin/v1/me` | Read the current administrator. |
| `GET`, `POST` | `/api/admin/v1/articles` | List or create articles. |
| `GET` | `/api/admin/v1/articles/{articleId}` | Read an article and its draft. |
| `PUT` | `/api/admin/v1/articles/{articleId}/draft` | Optimistically save the draft. |
| `GET` | `/api/admin/v1/articles/{articleId}/preview` | Read preview data. |
| `GET`, `POST` | `/api/admin/v1/articles/{articleId}/versions` | List or create immutable versions. |
| `POST` | `/api/admin/v1/articles/{articleId}/versions/{revisionId}/restore` | Restore a version into a new draft. |
| `POST` | `/api/admin/v1/articles/{articleId}/trash` | Trash an unpublished article. |
| `POST` | `/api/admin/v1/articles/{articleId}/untrash` | Restore a trashed article. |
| `GET`, `POST` | `/api/admin/v1/tags` | List or create tags. |
| `PATCH` | `/api/admin/v1/tags/{tagId}` | Rename a tag. |
| `POST` | `/api/admin/v1/media/upload-policy` | Create a direct-upload policy. |
| `POST` | `/api/admin/v1/media` | Register verified GFS media. |
| `GET`, `PUT` | `/api/admin/v1/settings/site` | Read or replace site settings. |
| `GET`, `PUT` | `/api/admin/v1/settings/hotlink` | Read or atomically replace hotlink settings. |
| `GET` | `/health/live` | Public liveness check. |
| `GET` | `/health/ready` | Public MySQL/Redis readiness check. |
| `GET` | `/img/proxy/{publicKey}` | Public Referer-checked media redirect. |

`GET /health/live`, `GET /health/ready`, and
`GET /img/proxy/{publicKey}` are public and do not run Admin session or Origin
middleware. The public media route applies the current Referer policy before
looking up a key. Empty Referer is allowed by default; the default exact enabled
hosts are `qiuxs.com` and `blog-admin.qiuxs.com`. Subdomains do not inherit
access. A successful hotlink-settings write invalidates the in-process cache
before its response, so the next image request observes the new policy.

For an allowed request, the service signs the GFS read URL locally, responds
with temporary HTTP 302 and `Cache-Control: no-store`, and sends an empty body.
It does not make a read-signing request and does not proxy image bytes. GFS must
keep its final OSS hop as 302 or 307, never a permanent redirect.

## Site settings and filing gate

Before a row exists, site settings return virtual defaults: site and author name
`qiuxs`, an empty ordered social-link list, filing name `长安休息室`, filing
number `浙ICP备17057726号-1`, and lock version zero. Writes use optimistic locks;
social links must be canonical HTTPS URLs, and optional default SEO media must
already be active.

Site and author names are limited to 100 runes, author bio to 1000, home status
to 500, default SEO title to 100, default SEO description to 300, and each filing
field to 100. At most 16 ordered social links are accepted, with
case-insensitively unique labels. The About Markdown field and its HTTP request
limit follow the byte limits above.

The API always returns the fixed read-only filing link
`https://beian.miit.gov.cn/`; clients cannot configure or persist that URL.
`settings.ValidatePublishable` is the reusable gate requiring nonblank filing
name and number. Stage 3 owns Release enforcement and will invoke this
already-tested gate before creating a release snapshot. Stage 2 does not create
Release, Jenkins, Bundle, Astro, or deployment behavior.

## Generate, test, and build

Run from `service/`:

```sh
export GOTOOLCHAIN=go1.25.7
test "$(go env GOVERSION)" = "go1.25.7"
go mod download
make generate
git diff --exit-code -- internal/httpapi/admin.gen.go
go test ./...
go test -race ./internal/...
go test ./tests/flow/... -v
GOARCH=amd64 make build
file build/blog-service
```

`make build` writes a statically linked Linux amd64 executable to
`build/blog-service`; the directory is ignored by Git.

## Start and inspect health

For local development on the current operating system:

```sh
go run ./cmd/blog-service
```

On a Linux amd64 host after `make build`:

```sh
./build/blog-service
```

The process handles `SIGINT` and `SIGTERM`, allows 30 seconds for graceful
shutdown, and then exits. MySQL and Redis connections are owned and closed by
the process.

Health endpoints are public:

```sh
curl -i http://127.0.0.1:8080/health/live
curl -i http://127.0.0.1:8080/health/ready
```

`/health/live` reports process liveness. `/health/ready` returns 200 only when
both MySQL and Redis checks succeed; otherwise it returns 503.

## Design references

- [Service-foundation implementation plan](../docs/superpowers/plans/2026-08-13-service-foundation-auth.md)
- [Stage 2 content and media implementation plan](../docs/superpowers/plans/2026-08-13-service-content-media.md)
- [GFS media contract](../docs/contracts/gfs-blog-media.md)
- [Product and architecture design](../docs/superpowers/specs/2026-08-13-qiuxs-blog-design.md)
- [Roadmap](../docs/superpowers/plans/2026-08-13-qiuxs-blog-roadmap.md)
