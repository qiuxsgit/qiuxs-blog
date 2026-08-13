# qiuxs-blog service

This directory contains the Go 1.25.7 service foundation: health endpoints,
administrator bootstrap, login/session/logout, MySQL repositories, and Redis ID,
session, and rate-limit state. Later blog-management APIs are not implemented in
this phase.

## Configuration

All runtime configuration is supplied through environment variables.

| Variable | Required/default | Validation and purpose |
| --- | --- | --- |
| `BLOG_ENV` | `development` | Environment name. `production` requires an HTTPS admin origin and enables Secure session cookies. |
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

Every entity ID is allocated from a Redis counter before its MySQL `INSERT`.
IDs are positive signed `BIGINT` values and never encode timestamps or other
business data. `IDGEN_OFFSET` and `IDGEN_STEP` reserve deterministic lanes; the
default single lane produces `1, 2, 3, ...`.

## Manual SQL lifecycle

The binaries never read or execute files under `sqls/`. There is no migration
command and no automatic migration at startup.

For a release environment:

1. Review the immutable files under `sqls/releases/`.
2. Identify versions not yet recorded as applied in that environment.
3. Manually execute those files against MySQL in ascending semantic-version
   order before starting the binary that depends on them.
4. Record the applied versions through the deployment's operational process.

Never edit an existing release SQL file. `sqls/develop/develop.sql` collects
unreleased DDL; it is not a release migration. For a disposable local database
before a release file exists, review and execute that development SQL manually.
See [the SQL lifecycle notes](sqls/README.md).

## Initialize the first administrator

Prepare MySQL manually, start Redis, export the configuration above, then run:

```sh
go run ./cmd/blog-admin init --username qiuxs
```

The command prompts for the password twice without echoing it. It creates only
the first administrator; the MySQL singleton constraint remains authoritative
if initializers race.

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
- [Product and architecture design](../docs/superpowers/specs/2026-08-13-qiuxs-blog-design.md)
- [Roadmap](../docs/superpowers/plans/2026-08-13-qiuxs-blog-roadmap.md)
