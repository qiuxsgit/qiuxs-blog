# Service Foundation and Admin Authentication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a production-shaped Go service that boots from validated configuration, owns MySQL migrations, generates signed primary keys through Redis, initializes the single administrator, stores sessions and login limits in Redis, and exposes tested login/logout/current-admin and health endpoints.

**Architecture:** `service/` is one Go module split into focused platform, ID-generation, auth, migration, HTTP, and application-composition packages. OpenAPI defines the browser-facing contract and generates Gin route/model code; domain services depend on small interfaces, while MySQL and Redis adapters stay at the boundary. One shared Redis-backed ID generator is injected into repositories before any persistent entity can be created. This phase intentionally stops after authentication so article, media, and release work can build on an independently testable service.

**Tech Stack:** Go `1.25.7`, Gin `v1.12.0`, `database/sql` + MySQL driver `v1.10.0`, go-redis `v9.22.0`, golang-migrate `v4.19.1`, oapi-codegen `v2.8.0`, Argon2id from x/crypto `v0.55.0`, Testify `v1.11.1`, sqlmock `v1.5.2`, miniredis `v2.38.0`.

## Global Constraints

- Keep the root layout exactly `admin/`, `service/`, `site/`, `contracts/`, `deploy/`, and `docs/`; do not add wrapper directories.
- Service builds use Go exactly `1.25.7`; `service/go.mod` contains `go 1.25.0` and `toolchain go1.25.7`.
- Before any local Service command, run `export GOTOOLCHAIN=go1.25.7`; Jenkins already provides that exact toolchain.
- Admin will use Jenkins Node.js exactly `20.19.4`; Site will use Docker Node.js exactly `22.20.0`.
- There is one administrator, no public registration, no role system, and no password-recovery flow.
- Every MySQL primary key is `BIGINT NOT NULL`; never use `UNSIGNED` or `AUTO_INCREMENT`. Go and OpenAPI IDs use `int64`.
- Redis ID generation emits positive IDs only. Zero and negative values remain reserved, with no special meaning in this phase.
- IDs are identity only: never use `ORDER BY id` to express time. Sort by an explicit timestamp and use ID only as a stable tie-breaker.
- Passwords use Argon2id. Sessions and login-rate state live in Redis. MySQL is the administrator identity source of truth.
- Admin cookies are host-only, `HttpOnly`, `Secure`, `SameSite=Strict`, and scoped to `/api/admin/v1`.
- Unsafe Admin requests require the exact configured Origin. Do not enable CORS for Admin APIs.
- Follow TDD: write the focused test, observe its expected failure, implement the minimum behavior, and rerun the test.
- Automated tests in this phase must not start Docker containers or connect to deployed MySQL/Redis. Use sqlmock, miniredis, fakes, and `httptest` only.
- Never log passwords, password hashes, session tokens, Redis Session values, or secret environment values.

---

## Planned File Map

```text
contracts/openapi/admin-v1.yaml              Admin authentication contract
service/go.mod                               Go module and exact toolchain
service/Makefile                             Generate, test and build entry points
service/oapi-codegen.yaml                    Gin/model generation configuration
service/cmd/blog-service/main.go             Service process entry point
service/cmd/blog-migrate/main.go             Explicit migration command
service/cmd/blog-admin/main.go               First-admin initialization command
service/internal/app/app.go                  Dependency composition and router assembly
service/internal/auth/model.go               Auth values and errors
service/internal/auth/password.go            Argon2id
service/internal/auth/repository*.go          Repository contract and MySQL adapter
service/internal/auth/session*.go             Session manager and Redis adapter
service/internal/auth/limiter*.go             Limiter contract and Redis adapter
service/internal/auth/service.go              Auth orchestration
service/internal/bootstrap/admin.go          First-admin creation rule
service/internal/config/config.go            Environment parsing and validation
service/internal/health/handler.go            Liveness and readiness
service/internal/httpapi/admin.gen.go         Generated OpenAPI models and Gin routes
service/internal/httpapi/auth_handler.go      Generated interface implementation
service/internal/httpapi/auth_middleware.go   Session authentication
service/internal/httpapi/origin_middleware.go Unsafe-request Origin guard
service/internal/httpapi/problem.go           Stable problem responses
service/internal/httpapi/request_id.go        Request-ID middleware
service/internal/idgen/generator.go           Redis ID allocation and bounded conflict healing
service/internal/idgen/redis_counter.go       Atomic Redis INCR/Raise adapter
service/internal/migrate/migrate.go           Embedded golang-migrate runner
service/internal/migrate/migrations/*.sql     Canonical migrations
service/internal/platform/mysql.go            MySQL client
service/internal/platform/redis.go            Redis client
service/tests/flow/auth_test.go               In-process HTTP auth flow
```

Unit tests live beside production files as `*_test.go`. Keep password hashing, Session storage, login limiting, HTTP handling, and app composition in separate files.

---

### Task 1: Establish the Go Module and OpenAPI Contract

**Files:**
- Create: `service/go.mod`
- Create: `service/Makefile`
- Create: `service/oapi-codegen.yaml`
- Create: `contracts/openapi/admin-v1.yaml`
- Create: `service/internal/httpapi/contract_test.go`
- Generate: `service/internal/httpapi/admin.gen.go`

**Interfaces:**
- Consumes: No application code.
- Produces: generated `httpapi.ServerInterface`, `LoginRequest`, `AdminView`, `Problem`, `RegisterHandlers`, and embedded OpenAPI document.

- [ ] **Step 1: Create the module, test dependencies, and failing contract test**

Create `service/go.mod`:

```go
module github.com/qiuxsgit/qiuxs-blog/service

go 1.25.0

toolchain go1.25.7
```

Create `service/internal/httpapi/contract_test.go` before the contract exists:

```go
package httpapi_test

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestAdminContractContainsAuthenticationOperations(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../../contracts/openapi/admin-v1.yaml")
	require.NoError(t, err)
	require.NoError(t, doc.Validate(context.Background()))
	require.Equal(t, "loginAdmin", doc.Paths.Find("/api/admin/v1/session").Post.OperationID)
	require.NotNil(t, doc.Paths.Find("/api/admin/v1/session").Delete)
	require.NotNil(t, doc.Paths.Find("/api/admin/v1/me").Get)
}
```

Install only the dependencies needed to compile this test:

```bash
cd service
go get github.com/getkin/kin-openapi@v0.146.0
go get github.com/stretchr/testify@v1.11.1
```

- [ ] **Step 2: Run the test and observe the missing-contract failure**

Run:

```bash
cd service
go test ./internal/httpapi -run TestAdminContractContainsAuthenticationOperations -v
```

Expected: FAIL specifically because `admin-v1.yaml` does not exist. Dependency-resolution, compilation, or import errors are not an acceptable RED result; fix those and rerun until the assertion reaches the missing-contract failure.

- [ ] **Step 3: Add dependencies, generator config, and the contract**

Run:

```bash
cd service
go get github.com/gin-gonic/gin@v1.12.0
go mod tidy
```

Create `service/oapi-codegen.yaml`:

```yaml
package: httpapi
output: internal/httpapi/admin.gen.go
generate:
  models: true
  gin-server: true
  embedded-spec: true
```

Create `contracts/openapi/admin-v1.yaml`:

```yaml
openapi: 3.0.3
info:
  title: qiuxs-blog Admin API
  version: 1.0.0
servers:
  - url: https://blog-admin.qiuxs.com
paths:
  /api/admin/v1/session:
    post:
      operationId: loginAdmin
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: '#/components/schemas/LoginRequest'}
      responses:
        '200':
          description: Authenticated administrator
          content:
            application/json:
              schema: {$ref: '#/components/schemas/AdminView'}
        '400': {$ref: '#/components/responses/ProblemResponse'}
        '401': {$ref: '#/components/responses/ProblemResponse'}
        '403': {$ref: '#/components/responses/ProblemResponse'}
        '429': {$ref: '#/components/responses/ProblemResponse'}
        '503': {$ref: '#/components/responses/ProblemResponse'}
    delete:
      operationId: logoutAdmin
      responses:
        '204': {description: Session removed or already absent}
        '403': {$ref: '#/components/responses/ProblemResponse'}
        '503': {$ref: '#/components/responses/ProblemResponse'}
  /api/admin/v1/me:
    get:
      operationId: getCurrentAdmin
      responses:
        '200':
          description: Current administrator
          content:
            application/json:
              schema: {$ref: '#/components/schemas/AdminView'}
        '401': {$ref: '#/components/responses/ProblemResponse'}
        '503': {$ref: '#/components/responses/ProblemResponse'}
components:
  responses:
    ProblemResponse:
      description: Request failed
      content:
        application/problem+json:
          schema: {$ref: '#/components/schemas/Problem'}
  schemas:
    LoginRequest:
      type: object
      additionalProperties: false
      required: [username, password]
      properties:
        username: {type: string, minLength: 1, maxLength: 64}
        password: {type: string, minLength: 1, maxLength: 256}
    AdminView:
      type: object
      additionalProperties: false
      required: [id, username]
      properties:
        id: {type: integer, format: int64, minimum: 1}
        username: {type: string}
    Problem:
      type: object
      additionalProperties: false
      required: [type, title, status, code, requestId]
      properties:
        type: {type: string, format: uri}
        title: {type: string}
        status: {type: integer}
        code: {type: string}
        requestId: {type: string}
```

Generate:

```bash
cd service
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 --config oapi-codegen.yaml ../contracts/openapi/admin-v1.yaml
go mod tidy
```

- [ ] **Step 4: Verify contract and deterministic generation**

```bash
cd service
go test ./internal/httpapi -run TestAdminContractContainsAuthenticationOperations -v
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 --config oapi-codegen.yaml ../contracts/openapi/admin-v1.yaml
git diff --exit-code -- internal/httpapi/admin.gen.go
```

Expected: PASS and no generated diff.

- [ ] **Step 5: Add stable Make targets**

Create `service/Makefile`:

```make
.PHONY: version-check generate test build

version-check:
	@test "$$(go env GOVERSION)" = "go1.25.7" || (echo "Go 1.25.7 required, got $$(go env GOVERSION)" && exit 1)

generate:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 --config oapi-codegen.yaml ../contracts/openapi/admin-v1.yaml

test:
	go test ./...

build: version-check test
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=$${GOARCH:?GOARCH must be set} go build -trimpath -ldflags="-s -w" -o build/blog-service ./cmd/blog-service
```

- [ ] **Step 6: Commit**

```bash
git add service/go.mod service/go.sum service/Makefile service/oapi-codegen.yaml service/internal/httpapi/admin.gen.go service/internal/httpapi/contract_test.go contracts/openapi/admin-v1.yaml
git commit -m "feat(service): establish admin API contract"
```

---

### Task 2: Add Validated Environment Configuration

**Files:**
- Create: `service/internal/config/config.go`
- Create: `service/internal/config/config_test.go`

**Interfaces:**
- Consumes: environment-value getter.
- Produces: `config.Load(getenv func(string) string) (config.Config, error)` and typed HTTP, MySQL, Redis, ID-generation, and Session configuration.

- [ ] **Step 1: Write failing configuration tests**

Create tests for valid production values, missing MySQL DSN, missing Redis address, malformed Redis DB, invalid Session TTL, non-HTTPS production origin, invalid ID offset/step, and malformed heal flag:

```go
func TestLoadProductionConfig(t *testing.T) {
	env := validEnv()
	got, err := config.Load(func(key string) string { return env[key] })
	require.NoError(t, err)
	require.Equal(t, ":9010", got.HTTP.Addr)
	require.Equal(t, "https://blog-admin.qiuxs.com", got.HTTP.AdminOrigin)
	require.True(t, got.Session.CookieSecure)
	require.Equal(t, 24*time.Hour, got.Session.TTL)
	require.Equal(t, 2, got.Redis.DB)
	require.Equal(t, int64(1), got.IDGen.Offset)
	require.Equal(t, int64(1), got.IDGen.Step)
	require.False(t, got.IDGen.Heal)
}

func TestLoadRejectsNonHTTPSProductionOrigin(t *testing.T) {
	env := validEnv()
	env["BLOG_ADMIN_ORIGIN"] = "http://blog-admin.qiuxs.com"
	_, err := config.Load(func(key string) string { return env[key] })
	require.ErrorContains(t, err, "https")
}
```

Use this shared helper in `config_test.go` so every key and expected parsing rule is explicit:

```go
func validEnv() map[string]string {
	return map[string]string{
		"BLOG_ENV":                 "production",
		"BLOG_HTTP_ADDR":           ":9010",
		"BLOG_MYSQL_DSN":           "blog:secret@tcp(mysql:3306)/qiuxs_blog?parseTime=true&loc=UTC",
		"BLOG_REDIS_ADDR":          "redis:6379",
		"BLOG_REDIS_PASSWORD":      "redis-secret",
		"BLOG_REDIS_DB":            "2",
		"IDGEN_OFFSET":             "1",
		"IDGEN_STEP":               "1",
		"IDGEN_HEAL":               "false",
		"BLOG_ADMIN_ORIGIN":        "https://blog-admin.qiuxs.com",
		"BLOG_SESSION_COOKIE_NAME": "qx_blog_session",
		"BLOG_SESSION_TTL":         "24h",
	}
}
```

- [ ] **Step 2: Run tests and observe the missing package failure**

```bash
cd service
go test ./internal/config -v
```

Expected: FAIL because `config.Load` is undefined.

- [ ] **Step 3: Implement typed configuration**

Create these exact types:

```go
type Config struct {
	Environment string
	HTTP        HTTPConfig
	MySQL       MySQLConfig
	Redis       RedisConfig
	IDGen       IDGenConfig
	Session     SessionConfig
}

type HTTPConfig struct { Addr, AdminOrigin string }
type MySQLConfig struct { DSN string }
type RedisConfig struct { Addr, Password string; DB int }
type IDGenConfig struct { Offset, Step int64; Heal bool }
type SessionConfig struct {
	CookieName   string
	CookieSecure bool
	TTL          time.Duration
}
```

Defaults are development, `:8080`, cookie `qx_blog_session`, Redis DB 0, ID offset/step `1/1`, heal false, and Session TTL 24 hours. Require MySQL DSN, Redis address, and one exact Admin origin. Reject origin userinfo, query, fragment, non-root path, and non-HTTPS production origins. Restrict Session TTL to 15 minutes through 7 days. Parse ID settings as signed 64-bit integers and require `1 <= Offset <= Step`; no configuration may enable zero or negative generated IDs.

- [ ] **Step 4: Run tests**

```bash
cd service
go test ./internal/config -v
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add service/internal/config
git commit -m "feat(service): validate runtime configuration"
```

---

### Task 3: Add MySQL, Redis, Request-ID, and Health Foundations

**Files:**
- Create: `service/internal/platform/mysql.go`
- Create: `service/internal/platform/mysql_test.go`
- Create: `service/internal/platform/redis.go`
- Create: `service/internal/platform/redis_test.go`
- Create: `service/internal/httpapi/request_id.go`
- Create: `service/internal/httpapi/request_id_test.go`
- Create: `service/internal/health/handler.go`
- Create: `service/internal/health/handler_test.go`

**Interfaces:**
- Consumes: Task 2 config types.
- Produces: `OpenMySQL`, `OpenRedis`, Request-ID middleware, and liveness/readiness registration.

- [ ] **Step 1: Write failing platform tests**

Install the exact adapters and test doubles first:

```bash
cd service
go get github.com/go-sql-driver/mysql@v1.10.0
go get github.com/redis/go-redis/v9@v9.22.0
go get github.com/google/uuid@v1.6.0
go get github.com/DATA-DOG/go-sqlmock@v1.5.2
go get github.com/alicebob/miniredis/v2@v2.38.0
```

Then use sqlmock and miniredis:

```go
func TestConfigureMySQLPool(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	platform.ConfigureMySQLPool(db)
	require.Equal(t, 10, db.Stats().MaxOpenConnections)
}

func TestOpenRedisUsesSelectedDatabase(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := platform.OpenRedis(config.RedisConfig{Addr: server.Addr(), DB: 3})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.Equal(t, 3, client.Options().DB)
}
```

- [ ] **Step 2: Run and observe undefined functions**

```bash
cd service
go test ./internal/platform -v
```

Expected: FAIL.

- [ ] **Step 3: Implement bounded clients**

`OpenMySQL` uses `sql.Open("mysql", cfg.DSN)`, a five-second Ping context, and:

```go
func ConfigureMySQLPool(db *sql.DB) {
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
}
```

`OpenRedis` builds `redis.NewClient`, pings with five-second timeout, and closes after a failed ping. Never log DSN or password.

- [ ] **Step 4: Write failing middleware and health tests**

Test that a valid `X-Request-ID` is preserved, absent/invalid IDs become UUIDs, and liveness/readiness return JSON without dependency error text:

```go
func TestRequestIDCreatesResponseHeader(t *testing.T) {
	router := gin.New()
	router.Use(httpapi.RequestID())
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"requestId": httpapi.RequestIDFrom(c)})
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.NotEmpty(t, recorder.Header().Get("X-Request-ID"))
}
```

Define a common health boundary that both database implementations can satisfy without leaking client-specific return types:

```go
type Checker interface {
	Check(context.Context) error
}

type CheckFunc func(context.Context) error

func (f CheckFunc) Check(ctx context.Context) error { return f(ctx) }
```

Inject fake checkers in tests. App composition later adapts MySQL with `health.CheckFunc(db.PingContext)` and Redis with `health.CheckFunc(func(ctx context.Context) error { return redisClient.Ping(ctx).Err() })`.

- [ ] **Step 5: Implement middleware and health routes**

- `GET /health/live` always returns `200 {"status":"ok"}`.
- `GET /health/ready` returns 200 only when both checkers pass, otherwise `503 {"status":"unavailable"}`.
- Incoming Request IDs must match `[A-Za-z0-9._-]{1,64}`; otherwise generate `uuid.NewString()`.

- [ ] **Step 6: Run tests**

```bash
cd service
go test ./internal/platform ./internal/httpapi ./internal/health -v
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add service/go.mod service/go.sum service/internal/platform service/internal/httpapi/request_id.go service/internal/httpapi/request_id_test.go service/internal/health
git commit -m "feat(service): add platform health foundations"
```

---

### Task 4: Add the Shared Redis Primary-Key Generator

**Files:**
- Create: `service/internal/idgen/generator.go`
- Create: `service/internal/idgen/generator_test.go`
- Create: `service/internal/idgen/redis_counter.go`
- Create: `service/internal/idgen/redis_counter_test.go`

**Interfaces:**
- Consumes: Redis, MySQL, and Task 2 ID-generation configuration.
- Produces: one shared `idgen.Generator`, atomic Redis counter operations, positive signed `int64` IDs, and optional bounded PRIMARY-conflict healing.

- [ ] **Step 1: Define counter and generator contracts in failing tests**

Use these exact public boundaries:

```go
type Counter interface {
	Increment(ctx context.Context, key string) (int64, error)
	Raise(ctx context.Context, key string, floor int64) (int64, error)
}

func New(counter Counter, db *sql.DB, offset, step int64, heal bool) (*Generator, error)
func (g *Generator) Next(ctx context.Context, table string) (int64, error)
func (g *Generator) Insert(ctx context.Context, table string, insert func(id int64) error) error
func (g *Generator) Heal(ctx context.Context, table string) error
func (g *Generator) HealEnabled() bool
func IsPKConflict(err error) bool
```

Table names passed to the generator are exact physical table-name constants. Tests require names to match `[a-z][a-z0-9_]{0,62}` so the name is safe in the `MAX(id)` query.

- [ ] **Step 2: Write failing allocation and overflow tests**

With a fake Counter, prove:

- Offset/step `1/1` emits `1, 2, 3`.
- Offset/step `2/3` emits `2, 5, 8`.
- Constructor rejects `offset < 1`, `step < 1`, and `offset > step` instead of coercing them.
- Invalid table names are rejected before touching Redis.
- Counter errors are wrapped and return no ID.
- A nonpositive raw counter or overflow beyond `math.MaxInt64` returns an error; it never wraps into zero or a negative number.
- Generated IDs are signed `int64` but always positive.

Run the focused tests and observe the undefined symbols:

```bash
cd service
go test ./internal/idgen -run 'New|Next' -v
```

Expected: FAIL.

- [ ] **Step 3: Implement Redis allocation with checked arithmetic**

Use key `idseq:<real-table-name>` and formula:

```text
raw = INCR(key)
id  = offset + (raw - 1) * step
```

Check `(raw - 1) <= (math.MaxInt64 - offset) / step` before multiplication. Do not use `uint64`, local fallback counters, timestamps, UUIDs, or MySQL auto-increment.

`RedisCounter.Increment` calls go-redis `Incr`. `RedisCounter.Raise` uses one Lua operation that compares nonnegative decimal strings and only raises a counter. Do not use Lua `tonumber`: Redis Lua numbers cannot exactly represent the full signed BIGINT range.

```lua
local function normalize(value)
  local normalized = string.gsub(value, '^0+', '')
  if normalized == '' then return '0' end
  return normalized
end
local current = normalize(redis.call('GET', KEYS[1]) or '0')
local floor = normalize(ARGV[1])
local should_raise = #current < #floor or (#current == #floor and current < floor)
if should_raise then
  redis.call('SET', KEYS[1], floor)
  return floor
end
return current
```

Pass a validated positive floor as its base-10 string, return a Redis bulk string, and parse it with `strconv.ParseInt`; this preserves all values through `math.MaxInt64`.

- [ ] **Step 4: Test Redis keying, atomic Raise, and concurrent uniqueness**

Using miniredis, assert:

- `Next(ctx, "admins")` writes only `idseq:admins`.
- Raising from 3 to 10 returns 10; attempting to raise to 7 keeps 10.
- After Raise(10), the next default-lane ID is 11.
- 100 concurrent `Next(ctx, "admins")` calls yield 100 distinct positive IDs under `go test -race`.

```bash
cd service
go test ./internal/idgen -run Redis -v
go test -race ./internal/idgen -run Concurrent -v
```

Expected: PASS after the adapter is implemented.

- [ ] **Step 5: Write failing PRIMARY-conflict and healing tests**

Use `mysql.MySQLError` and sqlmock to prove:

- Only MySQL error 1062 whose message names `PRIMARY` satisfies `IsPKConflict`.
- A business unique-key 1062 passes through unchanged.
- Heal disabled returns a clear wrapped PRIMARY-conflict error after one insert attempt.
- Heal enabled queries `SELECT MAX(id) FROM <validated-table>` and raises the raw Redis counter so the next generated ID is the smallest ID above MAX in the configured lane.
- An empty table heals to the configured offset.
- Healing is limited to five retries after the initial attempt; it cannot loop forever.
- A nil DB while healing is enabled returns a configuration error.

- [ ] **Step 6: Implement bounded healing and run the package**

Calculate the next lane value without unsigned conversion:

```go
func nextInLane(max, offset, step int64) (int64, error)
```

For a positive `max`, the result is the smallest signed ID greater than max where `(id-offset)%step == 0`. Convert that ID to the raw counter floor `(next-offset)/step`, call `Counter.Raise`, and let the following `Increment` issue the recovered ID. Reject arithmetic overflow.

`Insert` calls `Next`, invokes the closure, and only invokes `Heal` after `IsPKConflict`. Other errors return unchanged through wrapping so repositories can map named business indexes independently.

```bash
cd service
go test ./internal/idgen -v
go test -race ./internal/idgen -v
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add service/internal/idgen
git commit -m "feat(service): generate signed primary keys with redis"
```

---

### Task 5: Own the Initial MySQL Migration

**Files:**
- Create: `service/internal/migrate/migrations/000001_create_admins.up.sql`
- Create: `service/internal/migrate/migrations/000001_create_admins.down.sql`
- Create: `service/internal/migrate/migrate.go`
- Create: `service/internal/migrate/migrate_test.go`
- Create: `service/cmd/blog-migrate/main.go`

**Interfaces:**
- Consumes: `*sql.DB` from `platform.OpenMySQL` and `config.Load`.
- Produces: `migrate.Up(context.Context, *sql.DB) error`, `migrate.Status(context.Context, *sql.DB, io.Writer) error`, and `blog-migrate up|status` without creating any auto-increment metadata table.

- [ ] **Step 1: Write the migration and failing embedded-file test**

Create `service/internal/migrate/migrations/000001_create_admins.up.sql`:

```sql
CREATE TABLE admins (
    id BIGINT NOT NULL,
    singleton_key TINYINT NOT NULL DEFAULT 1,
    username VARCHAR(64) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'active',
    last_login_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_admins_singleton (singleton_key),
    UNIQUE KEY uk_admins_username (username),
    CONSTRAINT chk_admins_singleton CHECK (singleton_key = 1),
    CONSTRAINT chk_admins_state CHECK (state IN ('active', 'disabled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
```

Create `service/internal/migrate/migrations/000001_create_admins.down.sql`:

```sql
DROP TABLE admins;
```

Create `migrate_test.go` and require `migrate.Filenames()` to return the two names in lexical order before `Filenames` exists. Read the embedded Up SQL in the test and assert the primary key is exactly `id BIGINT NOT NULL`, with no `AUTO_INCREMENT` and no `BIGINT UNSIGNED`.

- [ ] **Step 2: Run and observe the undefined-function failure**

```bash
cd service
go test ./internal/migrate -v
```

Expected: FAIL because the embedded migration API is undefined.

- [ ] **Step 3: Embed and run golang-migrate migrations**

Use `github.com/golang-migrate/migrate/v4/source/iofs` and its MySQL driver. This driver creates `schema_migrations(version BIGINT NOT NULL PRIMARY KEY, dirty BOOLEAN NOT NULL)`, so migration bookkeeping also avoids unsigned and auto-increment IDs.

Implement the canonical embedded filesystem; do not create a second migration directory:

```go
//go:embed migrations/*.up.sql migrations/*.down.sql
var files embed.FS

func Up(ctx context.Context, db *sql.DB) error {
	runner, err := newRunner(ctx, db)
	if err != nil {
		return err
	}
	defer runner.Close()
	if err := runner.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
```

`newRunner` creates an iofs source, acquires a dedicated `*sql.Conn`, and passes it to `mysql.WithConnection`; closing the runner must release that connection without closing the caller's pool. Set the migration table name explicitly to `schema_migrations`. Each migration file contains one top-level SQL statement, so the normal application DSN does not enable `multiStatements`.

`Status` uses `runner.Version()` and prints exactly `version=<n> dirty=<true|false>\n`; an empty database reports version 0 and dirty false. A dirty migration exits as failure and must be repaired explicitly—never force a version during service boot.

- [ ] **Step 4: Add the explicit migration command**

`cmd/blog-migrate/main.go` loads config, opens MySQL, applies a 60-second context, and supports exactly `up` and `status`. Missing/unknown commands print `usage: blog-migrate <up|status>` and exit 2. Database or migration failures exit 1.

- [ ] **Step 5: Run tests and compile**

```bash
cd service
go get github.com/golang-migrate/migrate/v4@v4.19.1
go get github.com/golang-migrate/migrate/v4/database/mysql@v4.19.1
go get github.com/golang-migrate/migrate/v4/source/iofs@v4.19.1
go test ./internal/migrate -v
go test ./...
go build ./cmd/blog-migrate
```

Expected: PASS and the command compiles.

- [ ] **Step 6: Commit**

```bash
git add service/go.mod service/go.sum service/internal/migrate service/cmd/blog-migrate
git commit -m "feat(service): add managed database migrations"
```

---

### Task 6: Implement Argon2id Password Hashing

**Files:**
- Create: `service/internal/auth/model.go`
- Create: `service/internal/auth/password.go`
- Create: `service/internal/auth/password_test.go`

**Interfaces:**
- Consumes: plain password strings only at hash/verify boundaries.
- Produces: `DefaultPasswordHasher`, `PasswordHasher.Hash`, and `PasswordHasher.Verify`.

- [ ] **Step 1: Define auth values and failing password tests**

Create `model.go`:

```go
package auth

import "errors"

var (
	ErrAdminNotFound      = errors.New("admin not found")
	ErrAdminAlreadyExists = errors.New("admin already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrRateLimited        = errors.New("rate limited")
	ErrSessionNotFound    = errors.New("session not found")
)

type Admin struct {
	ID           int64
	Username     string
	PasswordHash string
	State        string
}
```

Tests require an encoded `$argon2id$v=19$...` value, successful verification, false for a wrong password, error for malformed encoding, and rejection of empty or greater-than-256-byte passwords.

- [ ] **Step 2: Run and observe missing hasher failures**

```bash
cd service
go test ./internal/auth -run Password -v
```

Expected: FAIL because `PasswordHasher` is undefined.

- [ ] **Step 3: Implement the hasher and strict parser**

Use:

```go
type PasswordHasher struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
	rand        io.Reader
}

func DefaultPasswordHasher() PasswordHasher {
	return PasswordHasher{
		memory: 64 * 1024, iterations: 3, parallelism: 2,
		saltLength: 16, keyLength: 32, rand: rand.Reader,
	}
}
```

Encode `$argon2id$v=19$m=65536,t=3,p=2$<raw-base64-salt>$<raw-base64-key>`. Parse and bound every number before allocation, derive with `argon2.IDKey`, and compare with `subtle.ConstantTimeCompare`.

- [ ] **Step 4: Run tests**

```bash
cd service
go get golang.org/x/crypto@v0.55.0
go test ./internal/auth -run Password -v
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add service/go.mod service/go.sum service/internal/auth/model.go service/internal/auth/password.go service/internal/auth/password_test.go
git commit -m "feat(service): hash admin passwords with argon2id"
```

---

### Task 7: Add the Admin Repository and Initialization Command

**Files:**
- Create: `service/internal/auth/repository.go`
- Create: `service/internal/auth/repository_mysql.go`
- Create: `service/internal/auth/repository_mysql_test.go`
- Create: `service/internal/bootstrap/admin.go`
- Create: `service/internal/bootstrap/admin_test.go`
- Create: `service/cmd/blog-admin/main.go`

**Interfaces:**
- Consumes: PasswordHasher, MySQL, the shared Task 4 ID generator, normalized username, and hidden password input.
- Produces: `auth.Repository`, `auth.NewMySQLRepository`, and `bootstrap.CreateFirstAdmin`.

- [ ] **Step 1: Define the repository and write failing SQL tests**

Create `repository.go`:

```go
type Repository interface {
	Count(ctx context.Context) (int, error)
	Create(ctx context.Context, username, passwordHash string) (Admin, error)
	FindByUsername(ctx context.Context, username string) (Admin, error)
	FindByID(ctx context.Context, id int64) (Admin, error)
	UpdateLastLogin(ctx context.Context, id int64, at time.Time) error
}
```

Construct the repository with:

```go
func NewMySQLRepository(db *sql.DB, ids *idgen.Generator) *MySQLRepository
```

With sqlmock and a fake Counter behind the generator, assert the INSERT explicitly supplies the signed generated ID and `singleton_key=1`. Assert exact parameters, `sql.ErrNoRows` → `ErrAdminNotFound`, the named indexes `uk_admins_username`/`uk_admins_singleton` → `ErrAdminAlreadyExists`, PRIMARY conflict remains detectable by `idgen.IsPKConflict`, and timestamps use UTC. Never map every MySQL 1062 to the business sentinel.

- [ ] **Step 2: Run and observe missing repository failures**

```bash
cd service
go test ./internal/auth -run MySQLRepository -v
```

Expected: FAIL.

- [ ] **Step 3: Implement explicit SQL persistence**

Use `database/sql`. Normalize with `strings.ToLower(strings.TrimSpace(username))`; validate `[a-z0-9._-]{3,64}`. Select only `id, username, password_hash, state`. `Create` calls `ids.Insert(ctx, "admins", ...)`, assigns the generated `int64` ID, and issues `INSERT INTO admins (id, singleton_key, username, password_hash, state) ...`; it never relies on `LastInsertId`. Wrap operational errors with `%w` while preserving sentinel errors and the underlying MySQL error for ID-generator inspection.

- [ ] **Step 4: Write failing first-admin tests**

With a fake Repository, prove:

- Zero admins creates one active admin with Argon2id hash.
- Existing admin returns `bootstrap.ErrAdminExists` without hashing/writing.
- Invalid username and passwords below 12 bytes fail.
- Returned Admin has an empty `PasswordHash`.

Required signature:

```go
func CreateFirstAdmin(
	ctx context.Context,
	repo auth.Repository,
	hasher auth.PasswordHasher,
	username string,
	password string,
) (auth.Admin, error)
```

- [ ] **Step 5: Implement the initialization CLI**

`blog-admin init --username qiuxs` loads config, MySQL, and Redis; constructs `RedisCounter` and the shared generator from `cfg.IDGen`; and injects it into the repository. It reads `BLOG_ADMIN_PASSWORD` when set; otherwise uses `term.ReadPassword` twice and rejects a mismatch. Redis allocation failure aborts creation without fallback. Print only numeric ID and normalized username.

- [ ] **Step 6: Run tests and compile**

```bash
cd service
go get golang.org/x/term@v0.45.0
go test ./internal/auth ./internal/bootstrap -v
go build ./cmd/blog-admin
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add service/go.mod service/go.sum service/internal/auth service/internal/bootstrap service/cmd/blog-admin
git commit -m "feat(service): initialize the single administrator"
```

---

### Task 8: Store Opaque Sessions in Redis

**Files:**
- Create: `service/internal/auth/session.go`
- Create: `service/internal/auth/session_test.go`
- Create: `service/internal/auth/session_redis.go`
- Create: `service/internal/auth/session_redis_test.go`

**Interfaces:**
- Consumes: Redis client, Session TTL, authenticated Admin, random source, and clock.
- Produces: SessionManager `Create/Get/Delete` and Redis SessionStore `Set/Get/Delete`.

- [ ] **Step 1: Define Session contracts and failing manager tests**

```go
type Session struct {
	AdminID   int64     `json:"adminId"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type SessionStore interface {
	Set(ctx context.Context, digest string, session Session, ttl time.Duration) error
	Get(ctx context.Context, digest string) (Session, error)
	Delete(ctx context.Context, digest string) error
}

func NewSessionManager(store SessionStore, ttl time.Duration, random io.Reader, now func() time.Time) SessionManager
func (m SessionManager) Create(ctx context.Context, admin Admin) (string, Session, error)
func (m SessionManager) Get(ctx context.Context, token string) (Session, error)
func (m SessionManager) Delete(ctx context.Context, token string) error
```

Tests prove the returned token encodes 32 random bytes with base64url, the store receives only SHA-256 hex, expiry equals clock + TTL, malformed tokens return `ErrSessionNotFound`, and delete is idempotent.

- [ ] **Step 2: Run and observe missing SessionManager**

```bash
cd service
go test ./internal/auth -run SessionManager -v
```

Expected: FAIL.

- [ ] **Step 3: Implement the manager**

Generate exactly 32 bytes with `io.ReadFull`, encode using `base64.RawURLEncoding`, and hash the complete token with SHA-256 before storage. Keep token-digest conversion unexported.

- [ ] **Step 4: Write failing Redis adapter tests**

Use miniredis and assert:

- Key `qiuxs-blog:session:<sha256-hex>`.
- Value has only `adminId`, `username`, `expiresAt`.
- TTL is within one second of configured TTL.
- Missing/expired keys map to `ErrSessionNotFound`.

- [ ] **Step 5: Implement the Redis store and run tests**

Reject stored Session JSON with zero Admin ID, empty username, or past expiry even if Redis has not evicted it.

```bash
cd service
go test ./internal/auth -run 'SessionManager|RedisSessionStore' -v
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add service/go.mod service/go.sum service/internal/auth/session.go service/internal/auth/session_test.go service/internal/auth/session_redis.go service/internal/auth/session_redis_test.go
git commit -m "feat(service): store admin sessions in redis"
```

---

### Task 9: Rate-Limit Failed Logins in Redis

**Files:**
- Create: `service/internal/auth/limiter.go`
- Create: `service/internal/auth/limiter_redis.go`
- Create: `service/internal/auth/limiter_redis_test.go`

**Interfaces:**
- Consumes: normalized username, canonical client IP, Redis, and injected clock.
- Produces: `LoginLimiter.Allow`, `RecordFailure`, `ResetUsername`, and `NewRedisLoginLimiter`.

- [ ] **Step 1: Define the limiter and failing behavior tests**

```go
type LimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type LoginLimiter interface {
	Allow(ctx context.Context, username, ip string) (LimitDecision, error)
	RecordFailure(ctx context.Context, username, ip string) error
	ResetUsername(ctx context.Context, username string) error
}
```

With miniredis and a fixed clock, test a five-minute fixed window with maximum 5 failures per username and 20 per IP. Attempt 6 and 21 are denied; advancing to the next window allows requests. Redis keys contain SHA-256 digests, never literal username/IP.

- [ ] **Step 2: Run and observe missing limiter failures**

```bash
cd service
go test ./internal/auth -run RedisLoginLimiter -v
```

Expected: FAIL.

- [ ] **Step 3: Implement atomic failure counters**

Use this Lua script for username and IP keys:

```lua
local value = redis.call('INCR', KEYS[1])
if value == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return value
```

Key format: `qiuxs-blog:login:<kind>:<sha256-hex>:<window-start-unix>`. `Allow` reads both counters and returns remaining-window duration. Redis failure returns an error so login answers 503; never silently disable limiting.

- [ ] **Step 4: Run tests**

```bash
cd service
go test ./internal/auth -run RedisLoginLimiter -v
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add service/internal/auth/limiter.go service/internal/auth/limiter_redis.go service/internal/auth/limiter_redis_test.go
git commit -m "feat(service): rate limit failed admin logins"
```

---

### Task 10: Implement Authentication Service and HTTP Boundaries

**Files:**
- Create: `service/internal/auth/service.go`
- Create: `service/internal/auth/service_test.go`
- Create: `service/internal/httpapi/problem.go`
- Create: `service/internal/httpapi/origin_middleware.go`
- Create: `service/internal/httpapi/origin_middleware_test.go`
- Create: `service/internal/httpapi/auth_middleware.go`
- Create: `service/internal/httpapi/auth_middleware_test.go`
- Create: `service/internal/httpapi/auth_handler.go`
- Create: `service/internal/httpapi/auth_handler_test.go`

**Interfaces:**
- Consumes: Repository, PasswordHasher, SessionManager, LoginLimiter, generated API types, Session config, and exact Admin origin.
- Produces: Auth Service, generated ServerInterface implementation, Admin middleware, Origin guard, and Problem mapping.

- [ ] **Step 1: Write failing auth-Service tests**

Required API:

```go
type LoginResult struct {
	Admin   Admin
	Token   string
	Session Session
}

func NewService(repo Repository, hasher PasswordHasher, sessions SessionManager, limiter LoginLimiter, now func() time.Time) Service
func (s Service) Login(ctx context.Context, username, password, ip string) (LoginResult, error)
func (s Service) Logout(ctx context.Context, token string) error
func (s Service) Current(ctx context.Context, token string) (Admin, error)
```

Test success, unknown username, wrong password, disabled Admin, active rate limit, limiter failure, Session storage failure, idempotent logout, and current lookup after Admin disable. Unknown username and wrong password both return only `ErrInvalidCredentials`.

- [ ] **Step 2: Run and observe missing Service failures**

```bash
cd service
go test ./internal/auth -run Service -v
```

Expected: FAIL.

- [ ] **Step 3: Implement auth orchestration in a fixed order**

1. Normalize and validate username and password length.
2. Ask limiter whether username/IP may attempt.
3. Fetch Admin.
4. Verify Argon2id even for missing Admin using a package-level valid dummy hash.
5. On credential failure, record once and return `ErrInvalidCredentials`.
6. Treat a disabled Admin exactly like invalid credentials and record one failure; do not reveal account state.
7. Create the Redis Session.
8. Update `last_login_at`; if this fails, delete the new Session and return the dependency error.
9. Reset the username counter; if this fails, delete the new Session and return the dependency error.

These compensation steps ensure a reported failed login never leaves a usable Session behind. Session deletion during compensation is best effort but its failure is logged without the token or digest.

When `Allow` denies an attempt, return a typed `RateLimitError{RetryAfter time.Duration}` that unwraps to `ErrRateLimited`; the HTTP layer uses that duration without recalculating the window.

Clear `PasswordHash` before returning LoginResult or Current Admin.

- [ ] **Step 4: Write failing Origin and auth-middleware tests**

Prove:

- GET/HEAD do not require Origin.
- POST/PUT/PATCH/DELETE require exact `https://blog-admin.qiuxs.com`.
- Missing, `null`, malformed, subdomain, different port, HTTP, or multiple values return 403.
- Optional Session loading attaches the active Admin for a valid cookie.
- Missing, expired, or malformed Session leaves the context anonymous so Login and idempotent Logout remain usable.
- A separate `RequireAdmin` guard turns an anonymous context into a 401 Problem without redirect; `/me` uses this guard.

- [ ] **Step 5: Implement Problem, Origin, and Session middleware**

Use Problem type `https://qiuxs.com/problems/<code>`, `application/problem+json`, and request ID. Map errors exactly:

| Error | HTTP | Code |
| --- | --- | --- |
| invalid request | 400 | `invalid_request` |
| invalid credentials | 401 | `invalid_credentials` |
| unauthenticated | 401 | `unauthenticated` |
| bad Origin | 403 | `origin_forbidden` |
| rate limited | 429 | `login_rate_limited` |
| MySQL/Redis unavailable | 503 | `dependency_unavailable` |
| unexpected | 500 | `internal_error` |

Do not expose internal error text.
For 429 responses, set `Retry-After` to the limiter decision rounded up to whole seconds.

- [ ] **Step 6: Write failing generated-handler tests**

Using `httptest` and generated routes, require:

- Strict Login JSON rejects unknown fields and bodies over 16 KiB.
- Success sets cookie Path `/api/admin/v1`, HttpOnly, Secure, SameSite Strict, and no Domain.
- Response contains only Admin ID and username.
- Logout clears cookie with `Max-Age=0`, returns 204 even when Session is absent.
- `/me` returns context Admin.
- Errors validate against generated Problem schema.

- [ ] **Step 7: Implement AuthHandler and run boundaries**

Implement generated methods:

```go
func (h *AuthHandler) LoginAdmin(c *gin.Context)
func (h *AuthHandler) LogoutAdmin(c *gin.Context)
func (h *AuthHandler) GetCurrentAdmin(c *gin.Context)
```

Use `http.MaxBytesReader`, `json.Decoder.DisallowUnknownFields`, and Gin client IP only after app wiring configures trusted proxies explicitly.

```bash
cd service
go test ./internal/auth ./internal/httpapi -v
go test ./...
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add service/internal/auth service/internal/httpapi
git commit -m "feat(service): expose admin session authentication"
```

---

### Task 11: Wire the Service and Prove the Full Auth Flow

**Files:**
- Create: `service/internal/app/app.go`
- Create: `service/internal/app/app_test.go`
- Create: `service/cmd/blog-service/main.go`
- Create: `service/tests/flow/auth_test.go`
- Create: `service/README.md`
- Modify: `README.md`

**Interfaces:**
- Consumes: all Tasks 1–10 outputs.
- Produces: `app.Build`, runnable `blog-service`, operator documentation, and an in-process HTTP auth-flow proof backed by sqlmock and miniredis.

- [ ] **Step 1: Write a failing router-composition test**

With fake dependencies, require:

```go
func TestBuildRegistersPublicAndAdminRoutes(t *testing.T) {
	router := buildTestRouter(t)
	requireRoute(t, router, http.MethodGet, "/health/live")
	requireRoute(t, router, http.MethodGet, "/health/ready")
	requireRoute(t, router, http.MethodPost, "/api/admin/v1/session")
	requireRoute(t, router, http.MethodDelete, "/api/admin/v1/session")
	requireRoute(t, router, http.MethodGet, "/api/admin/v1/me")
}
```

Unknown routes return JSON 404 Problem, not Gin text.

- [ ] **Step 2: Run and observe missing app composition**

```bash
cd service
go test ./internal/app -v
```

Expected: FAIL.

- [ ] **Step 3: Compose dependencies and middleware**

```go
type Dependencies struct {
	DB     *sql.DB
	Redis  *redis.Client
	Logger *slog.Logger
	Random io.Reader
	Now    func() time.Time
}

func Build(cfg config.Config, deps Dependencies) (*gin.Engine, error)
```

Order:

1. Construct one `RedisCounter`, then one shared `idgen.Generator` from `cfg.IDGen`, `deps.Redis`, and `deps.DB`; inject it into the Admin repository. Later repositories receive this same instance.
2. `gin.New()` with JSON slog request logging and generic-Problem recovery.
3. Request-ID middleware.
4. Empty trusted-proxy list in this phase; deployment adds Nginx proxy CIDRs in Phase 6.
5. Health routes.
6. Build a zero-prefix Gin route group with the Origin guard; the guard passes safe methods and rejects unsafe requests without the exact configured Origin.
7. Add optional Session-loading middleware to that group. Missing, stale, or malformed cookies leave the context anonymous so Login and idempotent Logout still work; a valid cookie attaches the current active Admin.
8. Call generated `RegisterHandlers` with that zero-prefix group. OpenAPI already contains the complete `/api/admin/v1/...` paths, so using an `/api/admin/v1`-prefixed group would incorrectly duplicate the prefix.
9. `GetCurrentAdmin` requires the context Admin and returns 401 when anonymous. Every protected endpoint added in later phases performs the same guard. Login remains unauthenticated, and Logout deletes a Session when present but remains 204 when absent.

Do not run migrations during service boot. Production runs `blog-migrate up` before starting a new binary.

- [ ] **Step 4: Implement graceful process startup**

`cmd/blog-service/main.go` loads config, creates JSON slog, opens MySQL/Redis, builds router, and starts `http.Server` with:

```go
ReadHeaderTimeout: 5 * time.Second,
ReadTimeout:       15 * time.Second,
WriteTimeout:      30 * time.Second,
IdleTimeout:       60 * time.Second,
```

On SIGINT/SIGTERM, call Shutdown with 30-second context. Config, connection, build, listen, or shutdown failures exit nonzero.

- [ ] **Step 5: Write the in-process HTTP flow test**

Use sqlmock for repository queries, miniredis for ID generation, Sessions, and rate limits, plus `httptest.Server` for the real Gin router. Do not use build tags, Docker, Testcontainers, deployed services, or fixed network ports. Assert:

1. First-admin creation returns positive signed ID 1, advances miniredis key `idseq:admins`, and passes that exact ID to the mocked INSERT.
2. Login without Origin → 403.
3. Wrong password with Origin → 401 and miniredis limiter increment.
4. Correct credentials → 200 plus secure host-only cookie.
5. `/me` with cookie → same Admin ID.
6. Logout with Origin → 204.
7. Subsequent `/me` → 401.
8. `/health/ready` → 200 while both injected checkers pass.

Migration DDL constraints remain covered by the focused migration test: `admins.id` must be signed `BIGINT NOT NULL`, and the SQL must contain neither `UNSIGNED` nor `AUTO_INCREMENT`. A deployment smoke test against production-like infrastructure is deferred to Phase 6, not run by this phase's automated suite.

- [ ] **Step 6: Run generation, unit, race, flow, and build checks**

With Go `1.25.7`:

```bash
cd service
make generate
git diff --exit-code -- internal/httpapi/admin.gen.go
go test ./...
go test -race ./internal/...
go test ./tests/flow/... -v
GOARCH=amd64 make build
file build/blog-service
```

Expected: all tests PASS, generated file clean, and a statically linked Linux amd64 executable.

- [ ] **Step 7: Document operator commands**

`service/README.md` lists every environment variable, including ID offset/step/heal defaults and validation; explains that all entity IDs come from Redis and never encode time; and documents migration order, first-admin command, test/build commands, start command, and health routes. Root `README.md` links the design spec, roadmap, and Service README without claiming later phases are available.

- [ ] **Step 8: Commit**

```bash
git add service/internal/app service/cmd/blog-service service/tests/flow service/README.md README.md service/go.mod service/go.sum
git commit -m "feat(service): deliver authenticated service foundation"
```

---

## Phase Completion Gate

From a clean checkout with Go `1.25.7`; Docker and deployed dependencies are not required:

```bash
export GOTOOLCHAIN=go1.25.7
test "$(cd service && go env GOVERSION)" = "go1.25.7"
(cd service && make generate)
(cd service && git diff --exit-code -- internal/httpapi/admin.gen.go)
(cd service && go test ./...)
(cd service && go test -race ./internal/...)
(cd service && go test ./tests/flow/... -v)
(cd service && GOARCH=amd64 make build)
```

Manual smoke test:

1. Run `blog-migrate up` against empty MySQL.
2. Run `blog-admin init --username qiuxs` and enter the password twice.
3. Start `blog-service` with MySQL, Redis, and Admin origin configured.
4. Confirm `/health/live` and `/health/ready` return 200.
5. Login with curl using exact Origin and a cookie jar.
6. Read `/api/admin/v1/me`, logout, and confirm `/me` returns 401.

The phase is complete only when every command passes, smoke test succeeds, generated files are clean, logs contain no credentials/tokens, and no implementation changes remain uncommitted.
