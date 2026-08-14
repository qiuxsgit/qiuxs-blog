# Task 8 report: immutable release orchestration proof

## Requirements mapping

| Requirement | Evidence |
| --- | --- |
| Fully composed fake-only flow | `service/tests/flow/release_test.go` constructs `app.Build` with sqlmock, miniredis, httptest, and an injected Jenkins transport only. |
| Immutable Bundle | The flow freezes a snapshot, downloads both identity and gzip forms, changes the live-flow phase, and verifies later identity bytes and ETag match the first Bundle. Bundle reads are explicitly expected from immutable `releases`/`release_articles` rows only. |
| Callback security/idempotence | The flow signs canonical JSON with timestamp/nonce/body HMAC, drives queue/build/deploy/fail stages, and repeats the failed request with the same nonce without adding a second SQL callback expectation. |
| Failed-release pointer protection | sqlmock requires `site_state ... FOR UPDATE`, then the failed callback requires only the active-job clear. There is deliberately no current-release advance expectation. |
| Retry | The flow creates a second publish-job ID for the same release, triggers Jenkins with it, and rechecks the original Bundle ETag. |
| Operations documentation | `service/README.md` documents variables, secret/key handling, manual SQL lifecycle, Builder setup, Bundle, callback/retry/reconciliation, and Stage 6 deployment ownership. Root README links the release guide. |

## RED / GREEN

RED command:

```text
GOTOOLCHAIN=go1.25.7 go test ./tests/flow -run ImmutableRelease -v
# github.com/qiuxsgit/qiuxs-blog/service/tests/flow_test
tests/flow/release_test.go:14:12: undefined: newReleaseFlow
FAIL
```

The initial test expressed the desired flow before its test-only app fixture
existed. GREEN added no production behavior: only the composed fake fixture and
documentation. The same focused command subsequently exited zero and reported
`PASS` for `TestImmutableReleaseThroughJenkinsCallbackAndRetry`.

## Verification command summary

The required gate was run with `GOTOOLCHAIN=go1.25.7`: toolchain assertion,
generation/generated-file diff, all Go tests, internal race tests, flow tests,
vet, gofmt diff, Git whitespace check, and Linux amd64 static build/file check.
All commands exited zero (see the final task handoff for the concise gate line).

## Self-review

- No Docker, real MySQL/Redis/Jenkins, fixed port, or network dependency is
  used by the flow test.
- The injected Jenkins transport validates the required release/job trigger
  parameters; the Bundle test asserts Bearer authentication, identity and gzip.
- Documentation explicitly states that service never deploys files and Stage 6
  owns Jenkins/Nginx/SSH.
- No plan or SQL migration file was modified.

## Concerns

The fixture deliberately models immutable database snapshot rows through
sqlmock; it does not claim to substitute for a deployment-system integration
test. Stage 6 remains responsible for testing filesystem, Nginx, and SSH
behavior against its own pipeline boundaries.

## Round 1 fixture correction

Review identified that the original helper named
`mutateDraftAndSettingsOutsideRelease` did not perform a mutation, its current
release assertion compared an uninitialized field to itself, and the Jenkins
transport accepted any nonempty publish-job ID. The correction was test-only:

- The test first failed after requiring two live mutations: expected `2`, got
  `0`.
- GREEN drives `PUT /api/admin/v1/articles/41/draft` and
  `PUT /api/admin/v1/settings/site` through the composed Admin handler/domain/
  repository stack with sqlmock writes that change the live rows. Bundle reads
  after those writes still use the release snapshot rows and retain their
  identity bytes, gzip bytes after decompression, and ETag.
- The fixture starts with `current_release_id = 7`. Failed callback SQL is
  limited to the active-job clear after finalizing the failed job; it contains no
  current-release or published-article pointer update expectation, and the test
  asserts the observable old pointer remains `7`.
- The Jenkins transport records both parameter pairs. The test asserts one call
  for `(created release, created job)` and one for `(same release, retry job)`,
  and asserts retry returns the original release with a distinct job.
- Replay wording now correctly says that the identical canonical callback is
  accepted idempotently (HTTP 204), not rejected.
