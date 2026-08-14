# Service Release/Jenkins final fix report

Date: 2026-08-14

Branch: `main`

Reviewed base: `120cf2c`

Scope: Stage 3 Service Release/Jenkins only; no Admin implementation work and no automatic migration.

## Result

The approved bounded fix wave addresses all three blocking findings, all three
important findings, and the three requested minor findings. Production DDL
changes remain only in `service/sqls/develop/develop.sql`; the service still does
not read or execute development SQL.

## Findings and fixes

### C1 — consistent mutable snapshot and lock order

- Confirmed the original transaction mixed a repeatable-read snapshot with one
  locking draft read, allowing scalar and association rows to come from
  different database views.
- Release snapshot reads for site settings, article pointer, editing revision,
  tags, and media are now all current `SELECT ... FOR UPDATE` reads.
- Release and revision writers use article pointer -> current editing draft ->
  associations ordering after the release-only `site_state` lock.
- The frozen draft hash is checked with the existing
  `revision.ComputeHash(revision.PreparedContent)` projection using the locked
  title, summary, cover, Markdown, tags, and media. No hash algorithm was copied.
- Exact SQL/order tests and flow fixtures cover the contract and reject a stored
  hash that does not match the locked content before any freeze/write.

Manual gate: sqlmock cannot prove InnoDB scheduling. Before applying this DDL in
production, race a draft save and publish for the same article on real MySQL;
both transactions must finish without deadlock and the frozen revision must be
one complete before-save or after-save state with its canonical hash, never a
mixed scalar/tag/media state.

### C2 — callback retry after database failure

- A Redis same-digest duplicate no longer short-circuits with `204`; every
  authenticated canonical delivery enters `ApplyCallbackLocked`.
- The database row lock and exact callback match decide committed idempotency.
- A claimed nonce is never deleted after database failure.
- miniredis + httptest flow tests cover first database failure followed by the
  same canonical body succeeding, and concurrent same-body deliveries all
  reaching serialized repository idempotency.

### I1 — retry reconciliation ordering

- Retry now reconciles the deployed artifact before loading the builder,
  creating a retry job, or triggering Jenkins, matching publish ordering.
- Malformed, checksum-mismatched, and deployed-failed-attempt artifacts block
  before all downstream side effects.

### I2 — stable regular artifact open

- The production entry point now performs pre-open `lstat`, nonblocking
  `O_NOFOLLOW` open, descriptor `fstat`, post-open `lstat`, and inode identity
  checks.
- A symlinked parent such as `current` is allowed; a final-component symlink,
  directory, FIFO, device, or replacement race is rejected.
- Every after-open rejection closes the descriptor. `app.Build` remains
  filesystem-independent.
- Tests pass on Darwin, and the Linux amd64 static build compiles the same
  `x/sys/unix` implementation.

### I3 — immutable builder target history

- Each `publish_jobs` row stores the immutable non-secret target name, canonical
  HTTPS base URL, username, and job name used by that attempt.
- Builder config and job snapshots reuse `internal/buildtarget` canonical
  validation. Token and ciphertext are absent from the model, row snapshot, and
  OpenAPI view.
- Retry snapshots the currently enabled builder without mutating older jobs.
- The invalid release/build-number uniqueness constraint was removed; callback
  identity remains the exact positive Publish Job ID plus Release ID.
- SQL contract, repository, flow, generated OpenAPI, and secret-absence tests
  cover target persistence and a reused build number after changing builder.

### M1 — full `Accept-Encoding` selection

- Negotiation compares explicit gzip, wildcard, and identity q-values; ties
  prefer gzip and implicit identity retains q=1 unless explicitly excluded.
- Duplicate codings use the highest valid quality. An invalid member, duplicate
  q parameter, or unknown parameter invalidates only that member.
- If every supported representation has q=0, the endpoint returns documented
  `406 not_acceptable` before loading the bundle and includes
  `Vary: Accept-Encoding`.

### M2 — atomic bundle eligibility and rows

- `LoadBundleSnapshot` returns the ordered eligibility aggregate and immutable
  release articles from one repeatable-read read-only transaction.
- The service evaluates latest-job eligibility and cross-checks the bundle from
  that one snapshot, eliminating the two-transaction status race.
- An exact sqlmock transaction contract and the complete release flow cover the
  seam.

### M3 — safe release correlation logs

- Access logs support `release_id`, `publish_job_id`,
  `jenkins_build_number`, and `result`.
- Numeric values must be positive `int64`; result must be one of the six job
  status enums.
- Admin fields are added after session authentication, bundle fields only after
  the bearer middleware marks authentication, and callback fields only after a
  verified callback claim exists.
- No callback error summary, nonce, stage text, signature, Jenkins URL, token,
  or ciphertext is logged.

## TDD evidence

- C1 RED exposed non-locking site/tag/media reads and the joined pointer/draft
  lock; GREEN exact SQL tests now enforce all current reads and the unified
  writer order.
- C2 RED showed verifier duplicates skipped the repository; GREEN tests prove
  database-failure replay and concurrent duplicate dispatch.
- I1 RED showed retry began with builder loading; GREEN order is reconcile ->
  builder -> retry row -> trigger.
- I2 RED began with the undefined secure opener contract; GREEN covers regular,
  symlink-parent, special-file, race, and close behavior.
- I3 RED required target fields through the model/repository/API/DDL chain;
  GREEN covers immutable old/new targets and build-number reuse.
- M1 RED returned gzip when identity had higher q and returned 200 when all
  representations were disabled; GREEN covers the complete table.
- M2 RED failed at the removed two-call repository seam; GREEN uses one
  transaction.
- M3 RED failed on missing typed correlation accessors; GREEN filters invalid
  types, values, and arbitrary result strings.

## Verification evidence

All commands used `GOTOOLCHAIN=go1.25.7` where applicable.

- Toolchain assertion: `go env GOVERSION == go1.25.7`.
- Generated code: `make generate` reproduced
  `internal/httpapi/admin.gen.go` byte-for-byte (SHA-256
  `3b199a105718257c825bdfbb24ec4e3c8eac4a0b204abfdb8fd5f70ed8b6d78c`).
- Full suite: `go test ./...` passed.
- Internal race suite: `go test -race ./internal/...` passed.
- Flow race suite: `go test -race ./tests/flow/... -count=1` passed.
- Static analysis: `go vet ./...` passed.
- Formatting/whitespace: `gofmt` and `git diff --check` passed.
- Linux artifact: `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath
  ./cmd/blog-service` produced an ELF 64-bit x86-64 statically linked binary.

## Remaining operational gates

1. Perform the real-MySQL concurrency gate described under C1.
2. Review and manually execute the applicable forward-only SQL from
   `service/sqls/develop/develop.sql` according to the documented lifecycle.
3. Stage 6 remains responsible for Jenkins/Nginx/SSH deployment and rollback;
   this fix wave does not perform external deployment.
